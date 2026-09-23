package plugin

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

const (
	maxConcurrentProviderRequests = 4
	maxProviderRequestsPerWindow  = 30
	providerRequestWindow         = time.Minute
	maxLimiterIdentities          = 4096
	providerDialTimeout           = 10 * time.Second
)

var blockedProviderPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("2001:db8::/32"),
}

type lookupNetIPFunc func(context.Context, string, string) ([]netip.Addr, error)
type dialContextFunc func(context.Context, string, string) (net.Conn, error)

func newProviderTransport() *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// A proxy would resolve and dial the provider outside this process, bypassing
	// the destination checks below. Provider traffic therefore connects directly.
	transport.Proxy = nil
	dialer := &net.Dialer{Timeout: providerDialTimeout, KeepAlive: 30 * time.Second}
	transport.DialContext = secureProviderDialContext(net.DefaultResolver.LookupNetIP, dialer.DialContext)
	return transport
}

func secureProviderDialContext(lookup lookupNetIPFunc, dial dialContextFunc) dialContextFunc {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil || host == "" || port == "" {
			return nil, errors.New("provider address is invalid")
		}

		addresses, err := lookup(ctx, "ip", host)
		if err != nil || len(addresses) == 0 {
			return nil, errors.New("provider host could not be resolved")
		}

		allowPrivate := os.Getenv(allowHTTPEnv) == "true"
		for _, address := range addresses {
			if !allowPrivate && isBlockedProviderAddress(address) {
				return nil, errors.New("provider host resolves to a private or special-use network")
			}
		}

		var dialErrors []error
		for _, resolved := range addresses {
			connection, dialErr := dial(ctx, network, net.JoinHostPort(resolved.String(), port))
			if dialErr == nil {
				return connection, nil
			}
			dialErrors = append(dialErrors, dialErr)
		}
		return nil, fmt.Errorf("could not connect to provider: %w", errors.Join(dialErrors...))
	}
}

func isBlockedProviderAddress(address netip.Addr) bool {
	address = address.Unmap()
	if !address.IsValid() || !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() {
		return true
	}
	for _, prefix := range blockedProviderPrefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

func validateLiteralProviderHost(host string) error {
	address, err := netip.ParseAddr(host)
	if err != nil || os.Getenv(allowHTTPEnv) == "true" {
		return nil
	}
	if isBlockedProviderAddress(address) {
		return errors.New("base URL must not target a private or special-use network")
	}
	return nil
}

func requireGrafanaAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		user := backend.PluginConfigFromContext(request.Context()).User
		if user == nil || !strings.EqualFold(strings.TrimSpace(user.Role), "Admin") {
			http.Error(w, "administrator access required", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, request)
	})
}

type resourceRateWindow struct {
	started  time.Time
	lastSeen time.Time
	count    int
}

type providerResourceGate struct {
	concurrent  chan struct{}
	maxRequests int
	window      time.Duration
	now         func() time.Time

	mu      sync.Mutex
	windows map[string]*resourceRateWindow
}

func newProviderResourceGate(maxConcurrent, maxRequests int, window time.Duration) *providerResourceGate {
	return &providerResourceGate{
		concurrent:  make(chan struct{}, maxConcurrent),
		maxRequests: maxRequests,
		window:      window,
		now:         time.Now,
		windows:     make(map[string]*resourceRateWindow),
	}
}

func (gate *providerResourceGate) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if !gate.allow(requestIdentity(request.Context())) {
			w.Header().Set("Retry-After", strconv.Itoa(int(gate.window.Seconds())))
			http.Error(w, "provider request rate limit exceeded", http.StatusTooManyRequests)
			return
		}

		select {
		case gate.concurrent <- struct{}{}:
			defer func() { <-gate.concurrent }()
			next.ServeHTTP(w, request)
		default:
			w.Header().Set("Retry-After", "1")
			http.Error(w, "too many concurrent provider requests", http.StatusTooManyRequests)
		}
	})
}

func (gate *providerResourceGate) allow(identity string) bool {
	now := gate.now()
	gate.mu.Lock()
	defer gate.mu.Unlock()

	entry, exists := gate.windows[identity]
	if !exists {
		gate.prune(now)
		if len(gate.windows) >= maxLimiterIdentities {
			return false
		}
		entry = &resourceRateWindow{started: now}
		gate.windows[identity] = entry
	}
	if now.Before(entry.started) || now.Sub(entry.started) >= gate.window {
		entry.started = now
		entry.count = 0
	}
	entry.lastSeen = now
	if entry.count >= gate.maxRequests {
		return false
	}
	entry.count++
	return true
}

func (gate *providerResourceGate) prune(now time.Time) {
	for identity, entry := range gate.windows {
		if now.Sub(entry.lastSeen) >= 2*gate.window {
			delete(gate.windows, identity)
		}
	}
}

func requestIdentity(ctx context.Context) string {
	pluginContext := backend.PluginConfigFromContext(ctx)
	namespace := strings.TrimSpace(pluginContext.Namespace)
	if namespace == "" {
		namespace = "default"
	}
	if pluginContext.User == nil {
		return namespace + "\x00system"
	}
	identity := strings.TrimSpace(pluginContext.User.Login)
	if identity == "" {
		identity = strings.TrimSpace(pluginContext.User.Email)
	}
	if identity == "" {
		identity = strings.TrimSpace(pluginContext.User.Name)
	}
	if identity == "" {
		identity = "authenticated-user"
	}
	return namespace + "\x00" + identity
}
