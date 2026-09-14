// Package load measures what the gateway costs a tools/call, end to end, in one
// process: real MCP clients, the gateway's HTTP handler with JWT verification
// and production logging, and real MCP backends, all over loopback HTTP.
//
// It runs only when asked to:
//
//	MCP_GATE_LOAD=1 go test ./tests/load -run TestLoad -v -timeout 30m
//
// MCP_GATE_LOAD_DURATION sets how long each measurement runs (default 8s),
// MCP_GATE_LOAD_PROFILE names a directory for CPU and mutex profiles of the
// gateway under load.
//
// Clients, gateway and backends share the machine, so requests per second say
// more about the machine than the gateway. The figure that carries over is CPU
// per call: the gateway's cost is the difference between a call through it and
// a call straight to the backend.
package load

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/lestrrat-go/jwx/v3/jwt"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	crzap "sigs.k8s.io/controller-runtime/pkg/log/zap"

	gatev1alpha1 "forgejo.metapatrol.local/platform/mcp-gate/api/v1alpha1"
	"forgejo.metapatrol.local/platform/mcp-gate/internal/auth"
	"forgejo.metapatrol.local/platform/mcp-gate/internal/discovery"
	"forgejo.metapatrol.local/platform/mcp-gate/internal/gateway"
	"forgejo.metapatrol.local/platform/mcp-gate/internal/mcptest"
	"forgejo.metapatrol.local/platform/mcp-gate/internal/policy"
)

func TestMain(m *testing.M) {
	if os.Getenv("MCP_GATE_LOAD") == "" {
		os.Exit(0)
	}
	os.Exit(m.Run())
}

const issuerURL = "https://oauth.example.com/"

// issuer signs RS256 tokens and publishes its key, as the OAuth provider does.
type issuer struct {
	key  jwk.Key
	jwks *httptest.Server
}

func newIssuer(t *testing.T) *issuer {
	raw, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	key, err := jwk.Import(raw)
	require.NoError(t, err)
	require.NoError(t, key.Set(jwk.KeyIDKey, "load"))
	require.NoError(t, key.Set(jwk.AlgorithmKey, jwa.RS256()))
	public, err := key.PublicKey()
	require.NoError(t, err)
	set := jwk.NewSet()
	require.NoError(t, set.AddKey(public))
	jwks := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(set)
	}))
	t.Cleanup(jwks.Close)
	return &issuer{key: key, jwks: jwks}
}

func (i *issuer) token(t *testing.T, subject string) string {
	tok := jwt.New()
	now := time.Now()
	require.NoError(t, tok.Set(jwt.IssuerKey, issuerURL))
	require.NoError(t, tok.Set(jwt.SubjectKey, subject))
	require.NoError(t, tok.Set(jwt.IssuedAtKey, now))
	require.NoError(t, tok.Set(jwt.ExpirationKey, now.Add(time.Hour)))
	signed, err := jwt.Sign(tok, jwt.WithKey(jwa.RS256(), i.key))
	require.NoError(t, err)
	return string(signed)
}

// transport is shared by the load clients, with enough idle connections that the
// harness does not measure its own connection churn. Agents are separate
// machines; the churn of one client transport is not the gateway's.
func transport() *http.Transport {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.MaxIdleConns = 0
	tr.MaxIdleConnsPerHost = 4096
	return tr
}

type bearer struct {
	base  http.RoundTripper
	token string
}

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return b.base.RoundTrip(r)
}

type gateSetup struct {
	// subjects is how many subjects hold a grant: subject-0 … subject-n-1.
	subjects int
	// passthrough registers the backend with auth type Passthrough.
	passthrough bool
	// tools is how many tools the backend offers; all are granted by pattern.
	tools int
}

type gate struct {
	url      string
	gateway  *gateway.Gateway
	snapshot func() *policy.Snapshot
	issuer   *issuer
}

// newGate starts a backend and the gateway in front of it.
func newGate(t *testing.T, setup gateSetup) (*gate, *mcptest.Backend) {
	tools := make([]string, setup.tools)
	for i := range tools {
		tools[i] = fmt.Sprintf("tool_%d", i)
	}
	backend := mcptest.NewBackend(t, "backend", tools...)

	// The gateway's own client to the backend is built as its main builds it,
	// so connection reuse towards backends is measured as it is in production.
	registry := discovery.NewRegistry(discovery.Options{
		HTTPClient:     &http.Client{Transport: http.DefaultTransport.(*http.Transport).Clone()},
		ConnectTimeout: 10 * time.Second,
	})
	t.Cleanup(registry.Close)
	var backendAuth *gatev1alpha1.BackendAuth
	if setup.passthrough {
		backendAuth = &gatev1alpha1.BackendAuth{Type: gatev1alpha1.BackendAuthPassthrough}
	}
	registry.Ensure("backend", backend.URL, backendAuth)
	_, _, err := registry.Discover(context.Background(), "backend")
	require.NoError(t, err)

	// The gateway logs an audit record per call; production logs JSON.
	logger := crzap.New(crzap.WriteTo(io.Discard))
	gw := gateway.New(gateway.Options{Registry: registry, Logger: logger})
	iss := newIssuer(t)
	verifier, err := auth.NewVerifier(auth.Config{Issuer: issuerURL, JWKSURL: iss.jwks.URL})
	require.NoError(t, err)
	handler, err := gw.Handler(gateway.HandlerOptions{Verifier: verifier.Verify})
	require.NoError(t, err)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = gw.Start(ctx) }()

	servers := []gatev1alpha1.MCPServer{{ObjectMeta: metav1.ObjectMeta{Name: "backend"}}}
	toolSets := []gatev1alpha1.MCPToolSet{{
		ObjectMeta: metav1.ObjectMeta{Name: "all"},
		Spec:       gatev1alpha1.MCPToolSetSpec{ServerRef: "backend", Tools: []string{"tool_*"}},
	}}
	capabilities := []gatev1alpha1.MCPCapability{{
		ObjectMeta: metav1.ObjectMeta{Name: "all"},
		Spec:       gatev1alpha1.MCPCapabilitySpec{ToolSets: []string{"all"}},
	}}
	grants := make([]gatev1alpha1.MCPGrant, setup.subjects)
	for i := range grants {
		subject := fmt.Sprintf("subject-%d", i)
		grants[i] = gatev1alpha1.MCPGrant{
			ObjectMeta: metav1.ObjectMeta{Name: subject},
			Spec:       gatev1alpha1.MCPGrantSpec{Subject: subject, Capabilities: []string{"all"}},
		}
	}
	snapshot := func() *policy.Snapshot { return policy.NewSnapshot(servers, toolSets, capabilities, grants) }
	gw.SetPolicy(snapshot())
	return &gate{url: srv.URL + "/mcp", gateway: gw, snapshot: snapshot, issuer: iss}, backend
}

func connect(url, token string, tr http.RoundTripper) (*mcp.ClientSession, error) {
	rt := tr
	if token != "" {
		rt = bearer{base: tr, token: token}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return mcp.NewClient(&mcp.Implementation{Name: "load", Version: "test"}, nil).Connect(ctx,
		&mcp.StreamableClientTransport{Endpoint: url, HTTPClient: &http.Client{Transport: rt}}, nil)
}

// op is one measured operation of one worker.
type op func(ctx context.Context) error

type result struct {
	scenario    string
	concurrency int
	ok, failed  int
	elapsed     time.Duration
	cpu         time.Duration
	latencies   []time.Duration
	firstError  string
}

func (r result) percentile(p float64) time.Duration {
	if len(r.latencies) == 0 {
		return 0
	}
	return r.latencies[min(len(r.latencies)-1, int(float64(len(r.latencies))*p))]
}

func cpuTime() time.Duration {
	var ru syscall.Rusage
	_ = syscall.Getrusage(syscall.RUSAGE_SELF, &ru)
	return time.Duration(ru.Utime.Nano() + ru.Stime.Nano())
}

func duration() time.Duration {
	if d, err := time.ParseDuration(os.Getenv("MCP_GATE_LOAD_DURATION")); err == nil {
		return d
	}
	return 8 * time.Second
}

// measure runs concurrency workers, each built by worker, for a warm-up second
// and then for the measurement duration.
func measure(t *testing.T, scenario string, concurrency int, worker func(i int) op) result {
	ops := make([]op, concurrency)
	var wg sync.WaitGroup
	for i := range ops {
		wg.Go(func() { ops[i] = worker(i) })
	}
	wg.Wait()

	warmup := time.Second
	start := time.Now()
	measureFrom, measureTo := start.Add(warmup), start.Add(warmup+duration())
	perWorker := make([][]time.Duration, concurrency)
	var failed atomic.Int64
	var firstError atomic.Pointer[string]
	var cpuFrom time.Duration
	var cpuOnce sync.Once
	for i := range ops {
		wg.Go(func() {
			for {
				t0 := time.Now()
				if t0.After(measureTo) {
					return
				}
				if !t0.Before(measureFrom) {
					cpuOnce.Do(func() { cpuFrom = cpuTime() })
				}
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				err := ops[i](ctx)
				cancel()
				if t0.Before(measureFrom) {
					continue
				}
				if err != nil {
					failed.Add(1)
					msg := err.Error()
					firstError.CompareAndSwap(nil, &msg)
					continue
				}
				perWorker[i] = append(perWorker[i], time.Since(t0))
			}
		})
	}
	wg.Wait()
	r := result{scenario: scenario, concurrency: concurrency, elapsed: duration(), cpu: cpuTime() - cpuFrom, failed: int(failed.Load())}
	for _, l := range perWorker {
		r.latencies = append(r.latencies, l...)
	}
	sort.Slice(r.latencies, func(i, j int) bool { return r.latencies[i] < r.latencies[j] })
	r.ok = len(r.latencies)
	if msg := firstError.Load(); msg != nil {
		r.firstError = *msg
	}
	return r
}

type report struct {
	mu   sync.Mutex
	rows []result
}

func (rep *report) add(t *testing.T, r result) {
	rep.mu.Lock()
	rep.rows = append(rep.rows, r)
	rep.mu.Unlock()
	t.Logf("%-44s c=%-4d %8.0f req/s  p50 %-9s p99 %-9s cpu/call %-9s errors %d %s",
		r.scenario, r.concurrency, float64(r.ok)/r.elapsed.Seconds(),
		r.percentile(0.5).Round(10*time.Microsecond), r.percentile(0.99).Round(10*time.Microsecond),
		perCall(r), r.failed, r.firstError)
}

func perCall(r result) time.Duration {
	if r.ok == 0 {
		return 0
	}
	return (r.cpu / time.Duration(r.ok)).Round(time.Microsecond)
}

func (rep *report) print() {
	var b strings.Builder
	fmt.Fprintf(&b, "\n| scenario | concurrency | req/s | p50 | p90 | p99 | max | CPU/call | errors |\n|---|---:|---:|---:|---:|---:|---:|---:|---:|\n")
	for _, r := range rep.rows {
		fmt.Fprintf(&b, "| %s | %d | %.0f | %s | %s | %s | %s | %s | %d |\n",
			r.scenario, r.concurrency, float64(r.ok)/r.elapsed.Seconds(),
			ms(r.percentile(0.5)), ms(r.percentile(0.9)), ms(r.percentile(0.99)), ms(r.percentile(1)),
			perCall(r), r.failed)
	}
	fmt.Println(b.String())
}

func ms(d time.Duration) string { return fmt.Sprintf("%.2fms", float64(d.Microseconds())/1000) }

// sessions opens one session per worker and returns an op calling tool_0 on it.
func sessionCall(t *testing.T, url string, token func(i int) string, tr http.RoundTripper) func(i int) op {
	return func(i int) op {
		session, err := connect(url, token(i), tr)
		require.NoError(t, err)
		t.Cleanup(func() { _ = session.Close() })
		return func(ctx context.Context) error {
			res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "tool_0", Arguments: map[string]any{"q": "load"}})
			if err == nil && res.IsError {
				err = fmt.Errorf("tool error: %v", res.Content)
			}
			return err
		}
	}
}

func profile(t *testing.T, name string, fn func()) {
	dir := os.Getenv("MCP_GATE_LOAD_PROFILE")
	if dir == "" {
		fn()
		return
	}
	require.NoError(t, os.MkdirAll(dir, 0o755))
	f, err := os.Create(filepath.Join(dir, name+".cpu.pprof"))
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	runtime.SetMutexProfileFraction(10)
	defer runtime.SetMutexProfileFraction(0)
	require.NoError(t, pprof.StartCPUProfile(f))
	fn()
	pprof.StopCPUProfile()
	mf, err := os.Create(filepath.Join(dir, name+".mutex.pprof"))
	require.NoError(t, err)
	defer func() { _ = mf.Close() }()
	require.NoError(t, pprof.Lookup("mutex").WriteTo(mf, 0))
}

func TestLoad(t *testing.T) {
	rep := &report{}
	defer rep.print()
	tr := transport()
	concurrencies := []int{1, 16, 64, 256}

	t.Run("call", func(t *testing.T) {
		g, backend := newGate(t, gateSetup{subjects: 1, tools: 20})
		token := g.issuer.token(t, "subject-0")
		for _, c := range concurrencies {
			rep.add(t, measure(t, "tools/call direct to the backend", c, sessionCall(t, backend.URL, func(int) string { return "" }, tr)))
			run := func() {
				rep.add(t, measure(t, "tools/call through the gateway", c, sessionCall(t, g.url, func(int) string { return token }, tr)))
			}
			if c == 64 {
				profile(t, "gateway-call-c64", run)
			} else {
				run()
			}
		}
	})

	t.Run("many subjects", func(t *testing.T) {
		g, _ := newGate(t, gateSetup{subjects: 256, tools: 20})
		tokens := make([]string, 256)
		for i := range tokens {
			tokens[i] = g.issuer.token(t, fmt.Sprintf("subject-%d", i))
		}
		rep.add(t, measure(t, "tools/call, 256 subjects", 256, sessionCall(t, g.url, func(i int) string { return tokens[i] }, tr)))

		// A GitOps sync applying grant after grant: the policy changes every
		// 10ms while the calls go on, and every subject's tool list is synced.
		stop := make(chan struct{})
		var churn sync.WaitGroup
		churn.Go(func() {
			tick := time.NewTicker(10 * time.Millisecond)
			defer tick.Stop()
			for {
				select {
				case <-stop:
					return
				case <-tick.C:
					g.gateway.SetPolicy(g.snapshot())
				}
			}
		})
		rep.add(t, measure(t, "tools/call, 256 subjects, policy change/10ms", 256, sessionCall(t, g.url, func(i int) string { return tokens[i] }, tr)))
		close(stop)
		churn.Wait()
	})

	t.Run("passthrough", func(t *testing.T) {
		g, _ := newGate(t, gateSetup{subjects: 256, tools: 20, passthrough: true})
		tokens := make([]string, 256)
		for i := range tokens {
			tokens[i] = g.issuer.token(t, fmt.Sprintf("subject-%d", i))
		}
		rep.add(t, measure(t, "tools/call, Passthrough, 256 tokens", 256, sessionCall(t, g.url, func(i int) string { return tokens[i] }, tr)))
	})

	t.Run("list", func(t *testing.T) {
		g, backend := newGate(t, gateSetup{subjects: 1, tools: 50})
		token := g.issuer.token(t, "subject-0")
		list := func(url string, tok string) func(i int) op {
			return func(int) op {
				session, err := connect(url, tok, tr)
				require.NoError(t, err)
				t.Cleanup(func() { _ = session.Close() })
				return func(ctx context.Context) error {
					res, err := session.ListTools(ctx, nil)
					if err == nil && len(res.Tools) != 50 {
						err = fmt.Errorf("listed %d tools", len(res.Tools))
					}
					return err
				}
			}
		}
		rep.add(t, measure(t, "tools/list (50 tools) direct", 64, list(backend.URL, "")))
		rep.add(t, measure(t, "tools/list (50 tools) through the gateway", 64, list(g.url, token)))
	})

	// What an idle agent costs: a session held open, with its notification
	// stream, by a subject of its own, which gets a server of its own.
	t.Run("idle sessions", func(t *testing.T) {
		const n = 1000
		g, backend := newGate(t, gateSetup{subjects: n, tools: 20})
		tokens := make([]string, n)
		for i := range tokens {
			tokens[i] = g.issuer.token(t, fmt.Sprintf("subject-%d", i))
		}
		heap := func() (uint64, int) {
			runtime.GC()
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			return m.HeapInuse, runtime.NumGoroutine()
		}
		hold := func(what, url string, token func(int) string) {
			heapBefore, goroutinesBefore := heap()
			sessions := make([]*mcp.ClientSession, n)
			var wg sync.WaitGroup
			for i := range sessions {
				wg.Go(func() {
					session, err := connect(url, token(i), tr)
					require.NoError(t, err)
					sessions[i] = session
				})
			}
			wg.Wait()
			time.Sleep(2 * time.Second) // let the notification streams open
			heapAfter, goroutinesAfter := heap()
			t.Logf("%d idle sessions %s: %.1f KiB heap and %.1f goroutines per session, clients included",
				n, what, float64(heapAfter-heapBefore)/n/1024, float64(goroutinesAfter-goroutinesBefore)/n)
			for _, s := range sessions {
				_ = s.Close()
			}
			time.Sleep(2 * time.Second)
		}
		hold("straight to the backend", backend.URL, func(int) string { return "" })
		hold("through the gateway, one subject each", g.url, func(i int) string { return tokens[i] })
	})

	t.Run("a session per call", func(t *testing.T) {
		g, backend := newGate(t, gateSetup{subjects: 1, tools: 20})
		token := g.issuer.token(t, "subject-0")
		once := func(url, tok string) func(int) op {
			return func(int) op {
				return func(ctx context.Context) error {
					session, err := connect(url, tok, tr)
					if err != nil {
						return err
					}
					defer func() { _ = session.Close() }()
					_, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "tool_0"})
					return err
				}
			}
		}
		rep.add(t, measure(t, "connect + call + close, direct", 16, once(backend.URL, "")))
		rep.add(t, measure(t, "connect + call + close, through the gateway", 16, once(g.url, token)))
	})
}

