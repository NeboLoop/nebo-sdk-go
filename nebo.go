// Package nebo provides the SDK for building Nebo apps.
//
// A Nebo app is a compiled binary that serves gRPC to Nebo on a loopback TCP port.
// The SDK handles the listener, the app token check, shutdown when Nebo goes away, and
// protocol bridging — you just implement handler interfaces for the capabilities your
// app provides.
//
// Quick start:
//
//	app, _ := nebo.New()
//	app.RegisterTool(&MyTool{})
//	log.Fatal(app.Run())
package nebo

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	pb "github.com/neboloop/nebo-sdk-go/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// stopGrace is how long in-flight calls get to finish before the server is stopped hard.
const stopGrace = 5 * time.Second

// App is the main entry point for a Nebo app. It manages the gRPC server,
// capability registration, and lifecycle.
type App struct {
	env         *AppEnv
	server      *grpc.Server
	onConfigure func(map[string]string)
	hasHandlers bool
	mux         *http.ServeMux
}

// New creates a new Nebo App. It reads the NEBO_* environment variables and
// prepares the gRPC server. Returns ErrNoToken if NEBO_APP_TOKEN is not set.
func New() (*App, error) {
	env := loadEnv()
	if env.Token == "" {
		return nil, ErrNoToken
	}
	a := &App{env: env}
	a.server = grpc.NewServer(
		grpc.UnaryInterceptor(func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
			if err := a.authorize(ctx); err != nil {
				return nil, err
			}
			return handler(ctx, req)
		}),
		grpc.StreamInterceptor(func(srv any, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
			if err := a.authorize(ss.Context()); err != nil {
				return err
			}
			return handler(srv, ss)
		}),
	)
	return a, nil
}

// authorize refuses a call unless it carries "authorization: Bearer <NEBO_APP_TOKEN>".
func (a *App) authorize(ctx context.Context) error {
	md, _ := metadata.FromIncomingContext(ctx)
	values := md.Get("authorization")
	if len(values) != 1 || subtle.ConstantTimeCompare([]byte(values[0]), []byte("Bearer "+a.env.Token)) != 1 {
		return status.Error(codes.Unauthenticated, "missing or invalid app token")
	}
	return nil
}

// Env returns the app's environment variables.
func (a *App) Env() *AppEnv {
	return a.env
}

// OnConfigure sets a callback that is called when Nebo pushes settings updates.
// This is shared across all capability handlers.
func (a *App) OnConfigure(fn func(map[string]string)) {
	a.onConfigure = fn
}

// RegisterTool registers a ToolHandler capability.
func (a *App) RegisterTool(h ToolHandler) {
	pb.RegisterToolServiceServer(a.server, &toolBridge{
		handler:     h,
		onConfigure: a.onConfigure,
		env:         a.env,
	})
	a.hasHandlers = true
}

// RegisterChannel registers a ChannelHandler capability.
func (a *App) RegisterChannel(h ChannelHandler) {
	pb.RegisterChannelServiceServer(a.server, &channelBridge{
		handler:     h,
		onConfigure: a.onConfigure,
		env:         a.env,
	})
	a.hasHandlers = true
}

// RegisterGateway registers a GatewayHandler capability.
func (a *App) RegisterGateway(h GatewayHandler) {
	pb.RegisterGatewayServiceServer(a.server, &gatewayBridge{
		handler:     h,
		onConfigure: a.onConfigure,
		env:         a.env,
	})
	a.hasHandlers = true
}

// HandleFunc registers an HTTP handler function for the given pattern.
// Identical to http.ServeMux.HandleFunc — same name, same signature.
// The app's HTTP handlers are called when the browser makes fetch() requests
// to /apps/{id}/api/{path}.
func (a *App) HandleFunc(pattern string, handler http.HandlerFunc) {
	if a.mux == nil {
		a.mux = http.NewServeMux()
	}
	a.mux.HandleFunc(pattern, handler)
	a.hasHandlers = true
}

// Handle registers an HTTP handler for the given pattern.
// Accepts any http.Handler — chi routers, gorilla/mux, middleware chains.
func (a *App) Handle(pattern string, handler http.Handler) {
	if a.mux == nil {
		a.mux = http.NewServeMux()
	}
	a.mux.Handle(pattern, handler)
	a.hasHandlers = true
}

// RegisterComm registers a CommHandler capability.
func (a *App) RegisterComm(h CommHandler) {
	pb.RegisterCommServiceServer(a.server, &commBridge{
		handler:     h,
		onConfigure: a.onConfigure,
		env:         a.env,
	})
	a.hasHandlers = true
}

// RegisterSchedule registers a ScheduleHandler capability.
func (a *App) RegisterSchedule(h ScheduleHandler) {
	pb.RegisterScheduleServiceServer(a.server, &scheduleBridge{
		handler:     h,
		onConfigure: a.onConfigure,
		env:         a.env,
	})
	a.hasHandlers = true
}

// Run serves gRPC on a free loopback port and blocks until Nebo goes away.
//
// It listens on 127.0.0.1:0, announces the address to Nebo with one stdout line
// ("nebo-app-listening <ip:port>"), and refuses every call that lacks the app token.
// It stops when stdin reaches EOF (Nebo is gone) or on SIGTERM/SIGINT.
func (a *App) Run() error {
	if !a.hasHandlers {
		return ErrNoHandlers
	}

	// Register UI service if HandleFunc/Handle was called
	if a.mux != nil {
		pb.RegisterUIServiceServer(a.server, &uiBridge{
			mux:         a.mux,
			onConfigure: a.onConfigure,
			env:         a.env,
		})
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(sigCh)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("listen on loopback: %w", err)
	}
	// The one line that tells Nebo where to connect.
	if _, err := fmt.Fprintf(os.Stdout, "nebo-app-listening %s\n", listener.Addr()); err != nil {
		listener.Close()
		return fmt.Errorf("announce listener: %w", err)
	}

	// Nebo holds our stdin open for our whole life; EOF means Nebo is gone.
	stdinClosed := make(chan struct{})
	go func(stdin io.Reader) {
		io.Copy(io.Discard, stdin)
		close(stdinClosed)
	}(os.Stdin)

	go func() {
		select {
		case <-stdinClosed:
		case <-sigCh:
		}
		a.stop()
	}()

	if err := a.server.Serve(listener); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
		return err
	}
	return nil
}

// stop lets in-flight calls finish, then stops hard so a hung stream cannot keep the app alive.
func (a *App) stop() {
	done := make(chan struct{})
	go func() {
		a.server.GracefulStop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(stopGrace):
		a.server.Stop()
	}
}
