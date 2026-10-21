package nebo

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"

	pb "github.com/neboloop/nebo-sdk-go/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// uiBridge adapts HandleFunc/Handle to the pb.UIServiceServer gRPC interface,
// and answers Nebo's health probe.
type uiBridge struct {
	pb.UnimplementedUIServiceServer
	mux          *http.ServeMux // nil when the app registered no HTTP handlers
	onConfigure  func(map[string]string)
	env          *AppEnv
	sidecarToken string
	port         uint16 // the port Run() listens on and announced
}

// HealthCheck is Nebo's health probe. When the probe carries an x-nebo-challenge
// (a 32-byte hex nonce), the answer carries an x-nebo-proof header that only the
// holder of NEBO_APP_TOKEN can compute. A missing or malformed challenge gets no
// proof, which Nebo treats as a failed probe.
func (b *uiBridge) HealthCheck(ctx context.Context, _ *pb.HealthCheckRequest) (*pb.HealthCheckResponse, error) {
	if challenge := metadata.ValueFromIncomingContext(ctx, "x-nebo-challenge"); len(challenge) == 1 {
		if nonce, err := hex.DecodeString(challenge[0]); err == nil && len(nonce) == 32 {
			proof := healthProof(b.env.Token, b.sidecarToken, nonce, b.port)
			if err := grpc.SetHeader(ctx, metadata.Pairs("x-nebo-proof", proof)); err != nil {
				return nil, err
			}
		}
	}
	return &pb.HealthCheckResponse{
		Healthy: true,
		Name:    b.env.Name,
		Version: b.env.Version,
	}, nil
}

// healthProof is lowercase hex HMAC-SHA256, keyed with the app token, over
// nonce || sidecar token || port (2 bytes, big-endian).
func healthProof(appToken, sidecarToken string, nonce []byte, port uint16) string {
	mac := hmac.New(sha256.New, []byte(appToken))
	mac.Write(nonce)
	mac.Write([]byte(sidecarToken))
	mac.Write(binary.BigEndian.AppendUint16(nil, port))
	return hex.EncodeToString(mac.Sum(nil))
}

func (b *uiBridge) Configure(_ context.Context, req *pb.SettingsMap) (*pb.Empty, error) {
	if b.onConfigure != nil {
		b.onConfigure(req.Values)
	}
	return &pb.Empty{}, nil
}

// HandleRequest dispatches a proxied HTTP request through the app's http.ServeMux.
func (b *uiBridge) HandleRequest(ctx context.Context, req *pb.HttpRequest) (*pb.HttpResponse, error) {
	if b.mux == nil {
		return nil, status.Error(codes.Unimplemented, "no HTTP handlers registered")
	}

	// Build path with query string
	uri := req.Path
	if req.Query != "" {
		uri += "?" + req.Query
	}

	// Build *http.Request from proto
	var body io.Reader
	if len(req.Body) > 0 {
		body = strings.NewReader(string(req.Body))
	}
	httpReq, err := http.NewRequestWithContext(ctx, req.Method, uri, body)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "build request: %v", err)
	}
	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}

	// Dispatch through standard net/http
	rec := httptest.NewRecorder()
	b.mux.ServeHTTP(rec, httpReq)

	// Convert response
	result := rec.Result()
	respBody, _ := io.ReadAll(result.Body)
	result.Body.Close()

	headers := make(map[string]string, len(result.Header))
	for k := range result.Header {
		headers[k] = result.Header.Get(k)
	}

	return &pb.HttpResponse{
		StatusCode: int32(result.StatusCode),
		Headers:    headers,
		Body:       respBody,
	}, nil
}
