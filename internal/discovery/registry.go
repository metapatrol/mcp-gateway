package discovery

import (
	"context"
	"log/slog"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	mcpv1alpha1 "forgejo.metapatrol.local/platform/mcp-gateway/api/v1alpha1"
	"forgejo.metapatrol.local/platform/mcp-gateway/internal/credentials"
	"forgejo.metapatrol.local/platform/mcp-gateway/internal/metrics"
)

// Options configures a Registry.
type Options struct {
	// Implementation is how this process introduces itself to backends.
	Implementation *mcp.Implementation

	// HTTPClient carries the traffic to backends. It must not set a Timeout:
	// notification streams stay open, and a client-wide timeout would cut
	// them. Deadlines come from contexts. Defaults to NewHTTPClient.
	HTTPClient *http.Client

	// ConnectTimeout bounds the handshake with a backend. Defaults to 10s.
	ConnectTimeout time.Duration

	// OnToolsChanged is called when a backend announces that its tool list
	// changed. It runs on the SDK's notification goroutine and must not block.
	OnToolsChanged func(server string)

	// Secrets resolves the Secret references of spec.auth. Nil fails every
	// backend that has any.
	Secrets credentials.Secrets

	// Logger receives the SDK's own client logging. Nil discards it.
	Logger *slog.Logger
}

// Registry holds the backends of all registered MCPServers and the catalog
// last read from each. It is safe for concurrent use.
type Registry struct {
	opts     Options
	progress *progressRoutes

	mu      sync.RWMutex
	entries map[string]*entry
}

type entry struct {
	backend     *Backend
	catalog     *Catalog
	lastSuccess time.Time
	failures    int
	lastErr     error
}

// NewRegistry creates an empty Registry.
func NewRegistry(opts Options) *Registry {
	if opts.Implementation == nil {
		opts.Implementation = &mcp.Implementation{Name: "mcp-gateway"}
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = NewHTTPClient()
	}
	if opts.ConnectTimeout <= 0 {
		opts.ConnectTimeout = 10 * time.Second
	}
	return &Registry{opts: opts, progress: newProgressRoutes(), entries: map[string]*entry{}}
}

// Ensure registers a backend, or updates it.
//
// A changed endpoint replaces the backend outright and discards its catalog:
// what the old address offered says nothing about the new one, and serving it
// until the next discovery would route calls by a catalog the new backend never
// published.
//
// Changed auth replaces the backend too, since every session was opened with the
// old credentials, but keeps the catalog. The backend is the same one, and the
// discovery that follows replaces the catalog if the new credentials see
// another; dropping it in between would make every agent's tools vanish and
// reappear.
func (r *Registry) Ensure(name, endpoint string, auth *mcpv1alpha1.BackendAuth) *Backend {
	built := credentials.Build(auth, r.opts.Secrets, r.opts.HTTPClient)

	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.entries[name]
	if ok && e.backend.Endpoint() == endpoint && e.backend.auth.Fingerprint == built.Fingerprint {
		return e.backend
	}
	b := newBackend(name, endpoint, built, r.newClient(nil), r.listenClient(name), r.opts.HTTPClient, r.opts.ConnectTimeout)
	b.progress = r.progress
	switch {
	case !ok:
		r.entries[name] = &entry{backend: b}
	case e.backend.Endpoint() != endpoint:
		_ = e.backend.Close()
		r.entries[name] = &entry{backend: b}
	default:
		_ = e.backend.Close()
		e.backend = b
	}
	return b
}

// newClient builds an MCP client. With onToolsChanged it listens for
// notifications/tools/list_changed; without, connecting never opens a stream
// for them, which is what the clients that make requests want.
func (r *Registry) newClient(onToolsChanged func()) *mcp.Client {
	opts := &mcp.ClientOptions{
		Logger: r.opts.Logger,
		// The gateway offers backends nothing: no roots, no sampling, no
		// elicitation. The SDK advertises roots unless told otherwise.
		Capabilities: &mcp.ClientCapabilities{},
	}
	if onToolsChanged != nil {
		opts.ToolListChangedHandler = func(context.Context, *mcp.ToolListChangedRequest) { onToolsChanged() }
	} else {
		opts.ProgressNotificationHandler = func(_ context.Context, req *mcp.ProgressNotificationClientRequest) {
			r.progress.deliver(req.Params)
		}
	}
	return mcp.NewClient(r.opts.Implementation, opts)
}

// listenClient builds the client a backend's listener connects with, or nil
// when nobody wants to hear about changes.
func (r *Registry) listenClient(name string) *mcp.Client {
	if r.opts.OnToolsChanged == nil {
		return nil
	}
	onChanged := r.opts.OnToolsChanged
	return r.newClient(func() { onChanged(name) })
}

// Forget unregisters a backend and closes its session.
func (r *Registry) Forget(name string) {
	r.mu.Lock()
	e, ok := r.entries[name]
	delete(r.entries, name)
	r.mu.Unlock()
	if ok {
		_ = e.backend.Close()
	}
	metrics.ForgetServer(name)
}

// Discover reads the catalog of a registered backend.
//
// changed reports whether the catalog differs from the one held before, by
// revision. An unchanged catalog is not replaced, so Catalog keeps returning the
// same pointer.
func (r *Registry) Discover(ctx context.Context, name string) (catalog *Catalog, changed bool, err error) {
	r.mu.RLock()
	e, ok := r.entries[name]
	r.mu.RUnlock()
	if !ok {
		return nil, false, ErrClosed
	}
	backend := e.backend

	fresh, err := backend.ReadCatalog(ctx)

	r.mu.Lock()
	defer r.mu.Unlock()
	// The backend may have been replaced or forgotten while the request was
	// out. Its answer then belongs to an endpoint nobody asked about.
	cur, ok := r.entries[name]
	if !ok || cur.backend != backend {
		return nil, false, ErrClosed
	}
	e = cur
	if err != nil {
		metrics.RecordDiscovery(name, 0, err)
		e.failures++
		e.lastErr = err
		return e.catalog, false, err
	}

	metrics.RecordDiscovery(name, fresh.Len(), nil)
	e.failures = 0
	e.lastErr = nil
	e.lastSuccess = time.Now()
	if e.catalog != nil && e.catalog.Revision == fresh.Revision {
		return e.catalog, false, nil
	}
	e.catalog = fresh
	return fresh, true, nil
}

// Health is what a registry knows about one backend's recent discoveries.
type Health struct {
	// LastSuccess is when discovery last succeeded; zero if it never has.
	LastSuccess time.Time
	// Failures is the number of consecutive failed discoveries.
	Failures int
	// LastError is the error of the most recent failure, if the most recent
	// attempt failed.
	LastError error
}

// Catalog returns the last successfully discovered catalog of a backend, however
// old. Callers that must not serve a stale catalog check Health.
func (r *Registry) Catalog(name string) (*Catalog, Health, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.entries[name]
	if !ok || e.catalog == nil {
		return nil, Health{}, false
	}
	return e.catalog, Health{LastSuccess: e.lastSuccess, Failures: e.failures, LastError: e.lastErr}, true
}

// Backend returns a registered backend.
func (r *Registry) Backend(name string) (*Backend, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.entries[name]
	if !ok {
		return nil, false
	}
	return e.backend, true
}

// Names returns the registered backends, sorted.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.entries))
	for name := range r.entries {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Close closes every backend session.
func (r *Registry) Close() {
	r.mu.Lock()
	entries := r.entries
	r.entries = map[string]*entry{}
	r.mu.Unlock()
	for _, e := range entries {
		_ = e.backend.Close()
	}
}

// RetryDelay is how long to wait before rediscovering a backend after failures
// consecutive failed attempts: 5s, 10s, 20s and so on, up to 10m. Callers cap it
// further by the server's own interval, so a failing backend is never polled
// less often than a healthy one.
func RetryDelay(failures int) time.Duration {
	d := 5 * time.Second
	for i := 1; i < failures && d < 10*time.Minute; i++ {
		d *= 2
	}
	return min(d, 10*time.Minute)
}
