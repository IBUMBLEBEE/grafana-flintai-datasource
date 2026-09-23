package plugin

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

func TestBlockedProviderAddresses(t *testing.T) {
	for _, value := range []string{
		"0.1.2.3",
		"10.0.0.1",
		"100.64.0.1",
		"127.0.0.1",
		"169.254.169.254",
		"192.0.2.1",
		"198.18.0.1",
		"224.0.0.1",
		"240.0.0.1",
		"::1",
		"fc00::1",
		"fe80::1",
		"2001:db8::1",
	} {
		if !isBlockedProviderAddress(netip.MustParseAddr(value)) {
			t.Errorf("expected %s to be blocked", value)
		}
	}
	for _, value := range []string{"1.1.1.1", "8.8.8.8", "2606:4700:4700::1111"} {
		if isBlockedProviderAddress(netip.MustParseAddr(value)) {
			t.Errorf("expected %s to be allowed", value)
		}
	}
}

func TestSecureProviderDialRejectsPrivateAndMixedDNSAnswers(t *testing.T) {
	for _, addresses := range [][]netip.Addr{
		{netip.MustParseAddr("10.0.0.8")},
		{netip.MustParseAddr("1.1.1.1"), netip.MustParseAddr("127.0.0.1")},
	} {
		dialed := false
		dial := secureProviderDialContext(
			func(context.Context, string, string) ([]netip.Addr, error) { return addresses, nil },
			func(context.Context, string, string) (net.Conn, error) {
				dialed = true
				return nil, nil
			},
		)
		if _, err := dial(context.Background(), "tcp", "provider.example:443"); err == nil || !strings.Contains(err.Error(), "private or special-use") {
			t.Fatalf("expected blocked DNS answer, got %v", err)
		}
		if dialed {
			t.Fatal("blocked DNS answer reached the network dialer")
		}
	}
}

func TestSecureProviderDialPinsTheValidatedAddress(t *testing.T) {
	client, server := net.Pipe()
	defer server.Close()
	var dialedAddress string
	dial := secureProviderDialContext(
		func(context.Context, string, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("1.1.1.1")}, nil
		},
		func(_ context.Context, network, address string) (net.Conn, error) {
			if network != "tcp" {
				t.Fatalf("unexpected network: %s", network)
			}
			dialedAddress = address
			return client, nil
		},
	)
	connection, err := dial(context.Background(), "tcp", "provider.example:443")
	if err != nil {
		t.Fatalf("dial validated address: %v", err)
	}
	defer connection.Close()
	if dialedAddress != "1.1.1.1:443" {
		t.Fatalf("expected the validated IP to be dialed, got %q", dialedAddress)
	}
}

func TestProviderConfigRejectsPrivateLiteralHTTPS(t *testing.T) {
	config := validConfig("https://127.0.0.1/v1")
	if err := config.validate(); err == nil || !strings.Contains(err.Error(), "private or special-use") {
		t.Fatalf("expected private HTTPS address to be rejected, got %v", err)
	}

	t.Setenv(allowHTTPEnv, "true")
	if err := config.validate(); err != nil {
		t.Fatalf("expected explicit local-development opt-in to permit the private address: %v", err)
	}
}

func TestRequireGrafanaAdmin(t *testing.T) {
	var calls atomic.Int32
	handler := requireGrafanaAdmin(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))

	for _, test := range []struct {
		name   string
		user   *backend.User
		status int
	}{
		{name: "admin", user: &backend.User{Login: "admin", Role: "Admin"}, status: http.StatusNoContent},
		{name: "editor", user: &backend.User{Login: "editor", Role: "Editor"}, status: http.StatusForbidden},
		{name: "viewer", user: &backend.User{Login: "viewer", Role: "Viewer"}, status: http.StatusForbidden},
		{name: "missing user", status: http.StatusForbidden},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/test", nil)
			ctx := backend.WithPluginContext(request.Context(), backend.PluginContext{OrgID: 1, User: test.user})
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request.WithContext(ctx))
			if recorder.Code != test.status {
				t.Fatalf("expected status %d, got %d", test.status, recorder.Code)
			}
		})
	}
	if calls.Load() != 1 {
		t.Fatalf("protected handler was called %d times", calls.Load())
	}
}

func TestTransientResourceRoutesRequireAdmin(t *testing.T) {
	datasource := newDatasource(validConfig("https://api.example.test/v1"), http.DefaultClient)
	for _, path := range []string{"models/preview", "test"} {
		t.Run(path, func(t *testing.T) {
			var response *backend.CallResourceResponse
			err := datasource.CallResource(context.Background(), &backend.CallResourceRequest{
				PluginContext: backend.PluginContext{OrgID: 1, User: &backend.User{Login: "viewer", Role: "Viewer"}},
				Path:          path,
				Method:        http.MethodPost,
			}, backend.CallResourceResponseSenderFunc(func(value *backend.CallResourceResponse) error {
				response = value
				return nil
			}))
			if err != nil {
				t.Fatalf("call resource: %v", err)
			}
			if response == nil || response.Status != http.StatusForbidden {
				t.Fatalf("expected route-level admin enforcement, got %#v", response)
			}
		})
	}
}

func TestProviderResourceGateRateLimitResets(t *testing.T) {
	now := time.Date(2026, time.September, 23, 12, 0, 0, 0, time.UTC)
	gate := newProviderResourceGate(1, 2, time.Minute)
	gate.now = func() time.Time { return now }
	handler := gate.wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	request := requestWithUser(backend.User{Login: "alice", Role: "Viewer"})
	for _, expected := range []int{http.StatusNoContent, http.StatusNoContent, http.StatusTooManyRequests} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request.Clone(request.Context()))
		if recorder.Code != expected {
			t.Fatalf("expected status %d, got %d", expected, recorder.Code)
		}
	}

	now = now.Add(time.Minute)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request.Clone(request.Context()))
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("expected reset rate window, got %d", recorder.Code)
	}
}

func TestProviderResourceGateRejectsExcessConcurrency(t *testing.T) {
	gate := newProviderResourceGate(1, 10, time.Minute)
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	handler := gate.wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(entered)
		<-release
		w.WriteHeader(http.StatusNoContent)
	}))
	request := requestWithUser(backend.User{Login: "alice", Role: "Viewer"})

	go func() {
		defer close(done)
		handler.ServeHTTP(httptest.NewRecorder(), request)
	}()
	<-entered

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request.Clone(request.Context()))
	if recorder.Code != http.StatusTooManyRequests || recorder.Header().Get("Retry-After") != "1" {
		t.Fatalf("expected concurrency rejection, got status=%d retry-after=%q", recorder.Code, recorder.Header().Get("Retry-After"))
	}
	close(release)
	<-done
}

func requestWithUser(user backend.User) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/generate", nil)
	ctx := backend.WithPluginContext(request.Context(), backend.PluginContext{OrgID: 1, User: &user})
	return request.WithContext(ctx)
}
