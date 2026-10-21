package nebo

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	pb "github.com/neboloop/nebo-sdk-go/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	sidecarToken = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef" // inbound: Nebo → sidecar
	appToken     = "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210" // outbound: sidecar → Nebo's API
)

func TestNewRequiresSidecarToken(t *testing.T) {
	t.Setenv("NEBO_SIDECAR_TOKEN", "")
	t.Setenv("NEBO_APP_TOKEN", appToken)
	_, err := New()
	if err != ErrNoSidecarToken {
		t.Fatalf("expected ErrNoSidecarToken, got %v", err)
	}
}

func TestNewKeepsSidecarTokenOutOfUserCode(t *testing.T) {
	t.Setenv("NEBO_SIDECAR_TOKEN", sidecarToken)
	t.Setenv("NEBO_APP_TOKEN", appToken)
	t.Setenv("NEBO_APP_NAME", "test-app")

	app, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if app.sidecarToken != sidecarToken {
		t.Errorf("sidecarToken = %q, want %q", app.sidecarToken, sidecarToken)
	}
	if v, ok := os.LookupEnv("NEBO_SIDECAR_TOKEN"); ok {
		t.Errorf("NEBO_SIDECAR_TOKEN still in the environment: %q", v)
	}
	if app.Env().Token != appToken {
		t.Errorf("Env().Token = %q, want the outbound %q", app.Env().Token, appToken)
	}
	if app.env.Name != "test-app" {
		t.Errorf("Name = %q, want test-app", app.env.Name)
	}
}

func TestRunWithNoHandlers(t *testing.T) {
	t.Setenv("NEBO_SIDECAR_TOKEN", sidecarToken)

	app, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	err = app.Run()
	if err != ErrNoHandlers {
		t.Fatalf("expected ErrNoHandlers, got %v", err)
	}
}

type echoTool struct{}

func (echoTool) Name() string            { return "echo" }
func (echoTool) Description() string     { return "Echoes its input." }
func (echoTool) Schema() json.RawMessage { return json.RawMessage(`{}`) }
func (echoTool) Execute(_ context.Context, input json.RawMessage) (string, error) {
	return string(input), nil
}

type emptyGateway struct{}

func (emptyGateway) Stream(context.Context, *GatewayRequest) (<-chan GatewayEvent, error) {
	ch := make(chan GatewayEvent)
	close(ch)
	return ch, nil
}
func (emptyGateway) Cancel(context.Context, string) error { return nil }

// sidecarEnv makes the test binary act as a sidecar app (see TestMain).
const sidecarEnv = "NEBO_SDK_TEST_SIDECAR"

func TestMain(m *testing.M) {
	if os.Getenv(sidecarEnv) == "1" {
		app, err := New()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		app.RegisterTool(echoTool{})
		app.RegisterGateway(emptyGateway{})
		app.HandleFunc("GET /ping", func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, "pong") })
		if err := app.Run(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// sidecar is this test binary running as an app, launched the way Nebo launches one:
// stdin and stdout are pipes, and closing stdin means Nebo went away.
type sidecar struct {
	addr  string
	stdin io.WriteCloser
	done  chan struct{} // closed when the process exits
	err   error         // the process's exit error; read after done
}

func startSidecar(t *testing.T) *sidecar {
	t.Helper()
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), sidecarEnv+"=1", "NEBO_SIDECAR_TOKEN="+sidecarToken, "NEBO_APP_TOKEN="+appToken, "NEBO_APP_NAME=test-app")
	cmd.Stderr = os.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sidecar: %v", err)
	}

	s := &sidecar{stdin: stdin, done: make(chan struct{})}
	t.Cleanup(func() {
		stdin.Close()
		select {
		case <-s.done:
		case <-time.After(10 * time.Second):
			cmd.Process.Kill()
			t.Error("sidecar did not exit after stdin closed")
		}
	})

	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatalf("read announce line: %v", err)
	}
	go func() {
		io.Copy(io.Discard, stdout)
		s.err = cmd.Wait()
		close(s.done)
	}()

	addr, ok := strings.CutPrefix(strings.TrimSuffix(line, "\n"), "nebo-app-listening ")
	if !ok {
		t.Fatalf("announce line = %q, want prefix %q", line, "nebo-app-listening ")
	}
	s.addr = addr
	return s
}

func dial(t *testing.T, addr string) *grpc.ClientConn {
	t.Helper()
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial %s: %v", addr, err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func withAuth(t *testing.T, value string) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	if value == "" {
		return ctx
	}
	return metadata.AppendToOutgoingContext(ctx, "authorization", value)
}

func TestRunListensOnLoopbackAndAnnounces(t *testing.T) {
	r := startSidecar(t)

	host, port, err := net.SplitHostPort(r.addr)
	if err != nil {
		t.Fatalf("announced address %q: %v", r.addr, err)
	}
	if host != "127.0.0.1" {
		t.Errorf("host = %q, want 127.0.0.1", host)
	}
	if port == "0" || port == "" {
		t.Errorf("port = %q, want an OS-assigned port", port)
	}
}

func TestRunRefusesCallsWithoutSidecarToken(t *testing.T) {
	r := startSidecar(t)
	conn := dial(t, r.addr)
	tool := pb.NewToolServiceClient(conn)
	gateway := pb.NewGatewayServiceClient(conn)
	ui := pb.NewUIServiceClient(conn)

	for _, tc := range []struct {
		name string
		auth string
	}{
		{"missing", ""},
		{"wrong token", "Bearer " + strings.Repeat("f", len(sidecarToken))},
		{"outbound app token", "Bearer " + appToken},
		{"no scheme", sidecarToken},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tool.Name(withAuth(t, tc.auth), &pb.Empty{})
			if status.Code(err) != codes.Unauthenticated {
				t.Errorf("unary: got %v, want Unauthenticated", err)
			}

			_, err = ui.HealthCheck(withAuth(t, tc.auth), &pb.HealthCheckRequest{})
			if status.Code(err) != codes.Unauthenticated {
				t.Errorf("UI health check: got %v, want Unauthenticated", err)
			}

			stream, err := gateway.Stream(withAuth(t, tc.auth), &pb.GatewayRequest{})
			if err == nil {
				_, err = stream.Recv()
			}
			if status.Code(err) != codes.Unauthenticated {
				t.Errorf("stream: got %v, want Unauthenticated", err)
			}
		})
	}
}

func TestRunAcceptsCallsWithSidecarToken(t *testing.T) {
	r := startSidecar(t)
	conn := dial(t, r.addr)

	resp, err := pb.NewToolServiceClient(conn).Name(withAuth(t, "Bearer "+sidecarToken), &pb.Empty{})
	if err != nil {
		t.Fatalf("unary: %v", err)
	}
	if resp.Name != "echo" {
		t.Errorf("Name = %q, want echo", resp.Name)
	}

	// Nebo's health probe.
	health, err := pb.NewUIServiceClient(conn).HealthCheck(withAuth(t, "Bearer "+sidecarToken), &pb.HealthCheckRequest{})
	if err != nil {
		t.Fatalf("UI health check: %v", err)
	}
	if !health.Healthy {
		t.Error("UI health check: Healthy = false, want true")
	}

	stream, err := pb.NewGatewayServiceClient(conn).Stream(withAuth(t, "Bearer "+sidecarToken), &pb.GatewayRequest{})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	if _, err := stream.Recv(); err != io.EOF {
		t.Errorf("stream Recv: got %v, want io.EOF", err)
	}
}

func TestRunExitsWhenStdinCloses(t *testing.T) {
	r := startSidecar(t)

	r.stdin.Close()
	select {
	case <-r.done:
		if r.err != nil {
			t.Fatalf("sidecar exited with %v, want a clean exit", r.err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("sidecar did not exit after stdin closed")
	}
}
