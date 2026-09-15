package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/utils/ptr"

	gatev1alpha1 "forgejo.metapatrol.local/platform/mcp-gate/api/v1alpha1"
	"forgejo.metapatrol.local/platform/mcp-gate/internal/credentials"
	"forgejo.metapatrol.local/platform/mcp-gate/internal/mcptest"
)

// fakeSecrets holds Secrets in memory: name, then key, then value.
type fakeSecrets struct {
	mu   sync.Mutex
	data map[string]map[string]string
}

func newFakeSecrets() *fakeSecrets { return &fakeSecrets{data: map[string]map[string]string{}} }

func (s *fakeSecrets) Set(name, key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data[name] == nil {
		s.data[name] = map[string]string{}
	}
	s.data[name][key] = value
}

func (s *fakeSecrets) Value(_ context.Context, ref gatev1alpha1.SecretKeyRef) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	secret, ok := s.data[ref.Name]
	if !ok {
		return "", fmt.Errorf("secret %q not found", ref.Name)
	}
	value, ok := secret[ref.Key]
	if !ok {
		return "", fmt.Errorf("secret %q has no key %q", ref.Name, ref.Key)
	}
	return value, nil
}

func (s *fakeSecrets) Invalidate(gatev1alpha1.SecretKeyRef) bool { return true }

// tokenServer is an OAuth token endpoint for the client credentials grant.
type tokenServer struct {
	*httptest.Server
	issued atomic.Int64

	mu       sync.Mutex
	valid    map[string]bool
	lastForm url.Values
}

func newTokenServer(t *testing.T, clientID, clientSecret string) *tokenServer {
	t.Helper()
	ts := &tokenServer{valid: map[string]bool{}}
	ts.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		id, secret, ok := r.BasicAuth()
		if !ok {
			id, secret = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
		}
		if id != clientID || secret != clientSecret || r.PostForm.Get("grant_type") != "client_credentials" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"invalid_client"}`))
			return
		}
		token := fmt.Sprintf("cc-token-%d", ts.issued.Add(1))
		ts.mu.Lock()
		ts.valid[token] = true
		ts.lastForm = r.PostForm
		ts.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": token, "token_type": "bearer", "expires_in": 3600})
	}))
	t.Cleanup(ts.Close)
	return ts
}

func (ts *tokenServer) accepts(authorization string) bool {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	token, ok := strings.CutPrefix(authorization, "Bearer ")
	return ok && ts.valid[token]
}

func (ts *tokenServer) revoke(token string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	delete(ts.valid, token)
}

func (ts *tokenServer) form() url.Values {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.lastForm
}

// requests counts what reached a backend, by JSON-RPC method or HTTP method.
type requests struct {
	mu     sync.Mutex
	counts map[string]int
	auth   map[string][]string
}

func (rs *requests) record(r *http.Request) string {
	method := mcptest.RPCMethod(r)
	if method == "" {
		method = r.Method
	}
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if rs.counts == nil {
		rs.counts, rs.auth = map[string]int{}, map[string][]string{}
	}
	rs.counts[method]++
	rs.auth[method] = append(rs.auth[method], r.Header.Get("Authorization"))
	return method
}

func (rs *requests) count(method string) int {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.counts[method]
}

func (rs *requests) authorizations(method string) []string {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return append([]string(nil), rs.auth[method]...)
}

func callText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	require.Len(t, res.Content, 1)
	return res.Content[0].(*mcp.TextContent).Text
}

func TestAuth_APIKeyInAHeaderOfItsOwn(t *testing.T) {
	backend := mcptest.NewBackend(t, "memory", "memory_search")
	backend.Guard(func(r *http.Request) int {
		if r.Header.Get("X-API-Key") != "k-123" || r.Header.Get("Authorization") != "" {
			return http.StatusUnauthorized
		}
		return 0
	})
	secrets := newFakeSecrets()
	secrets.Set("memory-credentials", "API_KEY", "k-123")
	r := newTestRegistry(t, Options{Secrets: secrets})
	b := r.Ensure("memory", backend.URL, &gatev1alpha1.BackendAuth{
		Type: gatev1alpha1.BackendAuthAPIKey,
		APIKey: &gatev1alpha1.APIKeyAuth{
			SecretRef: gatev1alpha1.SecretKeyRef{Name: "memory-credentials", Key: "API_KEY"},
			Header:    "X-API-Key",
		},
	})

	catalog, _, err := r.Discover(context.Background(), "memory")
	require.NoError(t, err)
	assert.Equal(t, []string{"memory_search"}, catalog.Names())
	res, err := b.CallTool(context.Background(), &mcp.CallToolParams{Name: "memory_search"})
	require.NoError(t, err)
	assert.False(t, res.IsError)
}

func TestAuth_APIKeyDefaultsToABearerToken(t *testing.T) {
	backend := mcptest.NewBackend(t, "memory", "memory_search")
	backend.RequireBearer(map[string]string{"k-123": "gateway"})
	secrets := newFakeSecrets()
	secrets.Set("memory-credentials", "token", "k-123")
	r := newTestRegistry(t, Options{Secrets: secrets})
	b := r.Ensure("memory", backend.URL, &gatev1alpha1.BackendAuth{
		Type:   gatev1alpha1.BackendAuthAPIKey,
		APIKey: &gatev1alpha1.APIKeyAuth{SecretRef: gatev1alpha1.SecretKeyRef{Name: "memory-credentials", Key: "token"}},
	})

	res, err := b.CallTool(context.Background(), &mcp.CallToolParams{Name: "memory_search"})
	require.NoError(t, err)
	assert.Equal(t, "memory/memory_search {} as gateway", callText(t, res))
}

func clientCredentialsAuth(tokenURL string) *gatev1alpha1.BackendAuth {
	return &gatev1alpha1.BackendAuth{
		Type: gatev1alpha1.BackendAuthClientCredentials,
		ClientCredentials: &gatev1alpha1.ClientCredentialsAuth{
			TokenURL:        tokenURL,
			ClientIDRef:     &gatev1alpha1.SecretKeyRef{Name: "memory-client", Key: "CLIENT_ID"},
			ClientSecretRef: gatev1alpha1.SecretKeyRef{Name: "memory-client", Key: "CLIENT_SECRET"},
			Scopes:          []string{"memory.read", "memory.write"},
			Audience:        []string{"memory", "memory-admin"},
		},
	}
}

func TestAuth_ClientCredentials(t *testing.T) {
	ts := newTokenServer(t, "gateway-client", "s3cret")
	backend := mcptest.NewBackend(t, "memory", "memory_search")
	backend.Guard(func(r *http.Request) int {
		if !ts.accepts(r.Header.Get("Authorization")) {
			return http.StatusUnauthorized
		}
		return 0
	})
	secrets := newFakeSecrets()
	secrets.Set("memory-client", "CLIENT_ID", "gateway-client")
	secrets.Set("memory-client", "CLIENT_SECRET", "s3cret")
	r := newTestRegistry(t, Options{Secrets: secrets})
	b := r.Ensure("memory", backend.URL, clientCredentialsAuth(ts.URL))

	_, _, err := r.Discover(context.Background(), "memory")
	require.NoError(t, err)
	for range 3 {
		_, err := b.CallTool(context.Background(), &mcp.CallToolParams{Name: "memory_search"})
		require.NoError(t, err)
	}
	assert.Equal(t, int64(1), ts.issued.Load(), "one token serves every request until it expires")
	form := ts.form()
	assert.Equal(t, "memory.read memory.write", form.Get("scope"))
	assert.Equal(t, "memory memory-admin", form.Get("audience"))
}

// A token the provider revoked is replaced on the backend's first refusal, and
// the call goes through on the session it was made on.
func TestAuth_ARevokedTokenIsReplaced(t *testing.T) {
	ts := newTokenServer(t, "gateway-client", "s3cret")
	backend := mcptest.NewBackend(t, "memory", "memory_search")
	var seen requests
	backend.Guard(func(r *http.Request) int {
		seen.record(r)
		if !ts.accepts(r.Header.Get("Authorization")) {
			return http.StatusUnauthorized
		}
		return 0
	})
	secrets := newFakeSecrets()
	secrets.Set("memory-client", "CLIENT_ID", "gateway-client")
	secrets.Set("memory-client", "CLIENT_SECRET", "s3cret")
	r := newTestRegistry(t, Options{Secrets: secrets})
	b := r.Ensure("memory", backend.URL, clientCredentialsAuth(ts.URL))
	_, err := b.CallTool(context.Background(), &mcp.CallToolParams{Name: "memory_search"})
	require.NoError(t, err)

	ts.revoke("cc-token-1")
	res, err := b.CallTool(context.Background(), &mcp.CallToolParams{Name: "memory_search"})
	require.NoError(t, err)
	assert.False(t, res.IsError)
	assert.Equal(t, int64(2), ts.issued.Load())
	assert.Equal(t, 1, seen.count("initialize"), "the session must survive the refusal")
}

// The SDK ends a connection on an HTTP error it cannot decode. A refusal must
// not end a session every other caller shares.
func TestAuth_ARefusalLeavesTheSessionIntact(t *testing.T) {
	backend := mcptest.NewBackend(t, "memory", "memory_search")
	var refuse atomic.Bool
	var seen requests
	backend.Guard(func(r *http.Request) int {
		if seen.record(r) == "tools/call" && refuse.Load() {
			return http.StatusForbidden
		}
		return 0
	})
	r := newTestRegistry(t, Options{})
	b := r.Ensure("memory", backend.URL, nil)
	_, err := b.CallTool(context.Background(), &mcp.CallToolParams{Name: "memory_search"})
	require.NoError(t, err)

	refuse.Store(true)
	_, err = b.CallTool(context.Background(), &mcp.CallToolParams{Name: "memory_search"})
	var refused *RefusedError
	require.ErrorAs(t, err, &refused)
	assert.Equal(t, http.StatusForbidden, refused.StatusCode)

	refuse.Store(false)
	_, err = b.CallTool(context.Background(), &mcp.CallToolParams{Name: "memory_search"})
	require.NoError(t, err)
	assert.Equal(t, 1, seen.count("initialize"))
	assert.Equal(t, 3, seen.count("tools/call"), "a refused call is not retried")
}

func TestAuth_ABackendThatWantsCredentialsSaysSo(t *testing.T) {
	backend := mcptest.NewBackend(t, "memory", "memory_search")
	backend.RequireBearer(map[string]string{"k-123": "gateway"})
	r := newTestRegistry(t, Options{})
	r.Ensure("memory", backend.URL, nil)

	_, _, err := r.Discover(context.Background(), "memory")
	var refused *RefusedError
	require.ErrorAs(t, err, &refused)
	assert.Equal(t, http.StatusUnauthorized, refused.StatusCode)
	assert.Contains(t, err.Error(), "the backend refused a request without credentials (401 Unauthorized): set spec.auth")
}

// A credential that cannot be produced keeps the request from going out at
// all, and says which Secret is at fault.
func TestAuth_AMissingSecretIsNamed(t *testing.T) {
	backend := mcptest.NewBackend(t, "memory", "memory_search")
	var seen requests
	backend.Guard(func(r *http.Request) int { seen.record(r); return 0 })
	r := newTestRegistry(t, Options{Secrets: newFakeSecrets()})
	r.Ensure("memory", backend.URL, &gatev1alpha1.BackendAuth{
		Type:   gatev1alpha1.BackendAuthAPIKey,
		APIKey: &gatev1alpha1.APIKeyAuth{SecretRef: gatev1alpha1.SecretKeyRef{Name: "memory-credentials", Key: "token"}},
	})

	_, _, err := r.Discover(context.Background(), "memory")
	var credential *CredentialError
	require.ErrorAs(t, err, &credential)
	assert.Contains(t, err.Error(), `spec.auth: secret "memory-credentials" not found`)
	assert.Zero(t, seen.count("initialize"))
}

func TestAuth_WithoutSecretsAccessTheSpecFails(t *testing.T) {
	backend := mcptest.NewBackend(t, "memory", "memory_search")
	r := newTestRegistry(t, Options{})
	r.Ensure("memory", backend.URL, &gatev1alpha1.BackendAuth{
		Type:   gatev1alpha1.BackendAuthAPIKey,
		APIKey: &gatev1alpha1.APIKeyAuth{SecretRef: gatev1alpha1.SecretKeyRef{Name: "memory-credentials", Key: "token"}},
	})

	_, _, err := r.Discover(context.Background(), "memory")
	var credential *CredentialError
	require.ErrorAs(t, err, &credential)
}

func passthroughBackend(t *testing.T) (*mcptest.Backend, *requests) {
	t.Helper()
	backend := mcptest.NewBackend(t, "memory", "memory_search")
	backend.RequireBearer(map[string]string{
		"discovery-key": "gateway",
		"alice-token":   "alice",
		"bob-token":     "bob",
	})
	seen := &requests{}
	backend.Guard(func(r *http.Request) int { seen.record(r); return 0 })
	return backend, seen
}

func passthroughAuth() *gatev1alpha1.BackendAuth {
	return &gatev1alpha1.BackendAuth{
		Type: gatev1alpha1.BackendAuthPassthrough,
		Passthrough: &gatev1alpha1.PassthroughAuth{
			Discovery: &gatev1alpha1.DiscoveryAuth{
				APIKey: &gatev1alpha1.APIKeyAuth{SecretRef: gatev1alpha1.SecretKeyRef{Name: "memory-discovery", Key: "key"}},
			},
		},
	}
}

func TestAuth_PassthroughCallsAsTheCaller(t *testing.T) {
	backend, seen := passthroughBackend(t)
	secrets := newFakeSecrets()
	secrets.Set("memory-discovery", "key", "discovery-key")
	r := newTestRegistry(t, Options{Secrets: secrets, OnToolsChanged: func(string) {}})
	b := r.Ensure("memory", backend.URL, passthroughAuth())

	_, _, err := r.Discover(context.Background(), "memory")
	require.NoError(t, err, "catalog reads use the discovery credential")

	as := func(token string) (*mcp.CallToolResult, error) {
		ctx := credentials.WithCaller(context.Background(), "Bearer "+token)
		return b.CallTool(ctx, &mcp.CallToolParams{Name: "memory_search"})
	}
	res, err := as("alice-token")
	require.NoError(t, err)
	assert.Equal(t, "memory/memory_search {} as alice", callText(t, res))
	res, err = as("bob-token")
	require.NoError(t, err)
	assert.Equal(t, "memory/memory_search {} as bob", callText(t, res))
	res, err = as("alice-token")
	require.NoError(t, err)
	assert.Equal(t, "memory/memory_search {} as alice", callText(t, res))

	assert.Equal(t, 2, b.callerSessions(), "one session per caller token, reused")
	for _, authorization := range seen.authorizations("initialize") {
		assert.NotEmpty(t, authorization)
	}
	assert.Equal(t, 1, count(seen.authorizations("initialize"), "Bearer alice-token"), "alice's session is reused")
	assert.Equal(t, 1, count(seen.authorizations("initialize"), "Bearer bob-token"))
	for _, authorization := range seen.authorizations("tools/call") {
		assert.NotEqual(t, "Bearer discovery-key", authorization, "tool calls never carry the discovery credential")
	}

	// The listener opens the notification stream, with the discovery
	// credential; the connections that make requests do not, since one stream
	// per backend is all it takes to hear about changes.
	require.Eventually(t, func() bool { return seen.count(http.MethodGet) > 0 }, 5*time.Second, 20*time.Millisecond)
	time.Sleep(100 * time.Millisecond)
	for _, authorization := range seen.authorizations(http.MethodGet) {
		assert.Equal(t, "Bearer discovery-key", authorization)
	}
}

func TestAuth_PassthroughRefusesACallWithoutACaller(t *testing.T) {
	backend, _ := passthroughBackend(t)
	secrets := newFakeSecrets()
	secrets.Set("memory-discovery", "key", "discovery-key")
	r := newTestRegistry(t, Options{Secrets: secrets})
	b := r.Ensure("memory", backend.URL, passthroughAuth())

	_, err := b.CallTool(context.Background(), &mcp.CallToolParams{Name: "memory_search"})
	assert.ErrorIs(t, err, ErrNoCaller)
}

func TestAuth_PassthroughReportsARefusedCaller(t *testing.T) {
	backend, _ := passthroughBackend(t)
	secrets := newFakeSecrets()
	secrets.Set("memory-discovery", "key", "discovery-key")
	r := newTestRegistry(t, Options{Secrets: secrets})
	b := r.Ensure("memory", backend.URL, passthroughAuth())

	ctx := credentials.WithCaller(context.Background(), "Bearer mallory-token")
	_, err := b.CallTool(ctx, &mcp.CallToolParams{Name: "memory_search"})
	var refused *RefusedError
	require.ErrorAs(t, err, &refused)
	assert.Equal(t, http.StatusUnauthorized, refused.StatusCode)
	assert.Equal(t, "the caller's token", refused.Credential)

	_, _, err = r.Discover(context.Background(), "memory")
	assert.NoError(t, err, "one caller's refusal is not the backend's")
}

// A backend like engram lists its tools to anyone and wants a token only for
// calls. Passthrough without a discovery credential fits it.
func TestAuth_PassthroughWithOpenDiscovery(t *testing.T) {
	backend := mcptest.NewBackend(t, "memory", "memory_search")
	var seen requests
	backend.Guard(func(r *http.Request) int {
		if seen.record(r) == "tools/call" && r.Header.Get("Authorization") != "Bearer alice-token" {
			return http.StatusUnauthorized
		}
		return 0
	})
	r := newTestRegistry(t, Options{})
	b := r.Ensure("memory", backend.URL, &gatev1alpha1.BackendAuth{Type: gatev1alpha1.BackendAuthPassthrough})

	_, _, err := r.Discover(context.Background(), "memory")
	require.NoError(t, err)
	for _, authorization := range seen.authorizations("tools/list") {
		assert.Empty(t, authorization)
	}
	res, err := b.CallTool(credentials.WithCaller(context.Background(), "Bearer alice-token"), &mcp.CallToolParams{Name: "memory_search"})
	require.NoError(t, err)
	assert.False(t, res.IsError)
}

func TestBackend_IdleCallerSessionsAreClosed(t *testing.T) {
	backend := mcptest.NewBackend(t, "memory", "memory_search")
	r := newTestRegistry(t, Options{})
	b := r.Ensure("memory", backend.URL, &gatev1alpha1.BackendAuth{Type: gatev1alpha1.BackendAuthPassthrough})
	for _, token := range []string{"a", "b"} {
		_, err := b.CallTool(credentials.WithCaller(context.Background(), "Bearer "+token), &mcp.CallToolParams{Name: "memory_search"})
		require.NoError(t, err)
	}
	require.Equal(t, 2, b.callerSessions())

	// One of them is still busy with a call.
	_, release, err := b.connFor(credentials.WithCaller(context.Background(), "Bearer a"))
	require.NoError(t, err)
	b.sweepCallers(time.Now().Add(callerIdleTimeout + time.Minute))
	assert.Equal(t, 1, b.callerSessions(), "a session with a call in flight is kept")

	release()
	b.mu.Lock()
	b.lastSweep = time.Time{}
	b.mu.Unlock()
	b.sweepCallers(time.Now().Add(callerIdleTimeout + time.Minute))
	assert.Zero(t, b.callerSessions())
}

// Changed auth needs new sessions, and not a new catalog: the backend is the
// same, and tools must not vanish from agents' lists while it is re-read.
func TestRegistry_AuthChangeKeepsTheCatalog(t *testing.T) {
	backend := mcptest.NewBackend(t, "memory", "memory_search")
	secrets := newFakeSecrets()
	secrets.Set("memory-credentials", "token", "k-123")
	r := newTestRegistry(t, Options{Secrets: secrets})
	before := r.Ensure("memory", backend.URL, nil)
	_, _, err := r.Discover(context.Background(), "memory")
	require.NoError(t, err)

	auth := &gatev1alpha1.BackendAuth{
		Type:   gatev1alpha1.BackendAuthAPIKey,
		APIKey: &gatev1alpha1.APIKeyAuth{SecretRef: gatev1alpha1.SecretKeyRef{Name: "memory-credentials", Key: "token"}, Prefix: ptr.To("")},
	}
	after := r.Ensure("memory", backend.URL, auth)
	assert.NotSame(t, before, after)
	_, err = before.CallTool(context.Background(), &mcp.CallToolParams{Name: "memory_search"})
	assert.ErrorIs(t, err, ErrClosed)
	_, _, ok := r.Catalog("memory")
	assert.True(t, ok)

	assert.Same(t, after, r.Ensure("memory", backend.URL, auth.DeepCopy()), "an equal spec keeps the backend")
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Credentials go to the backend's host and nowhere else, redirects included.
func TestAuthTransport_CredentialsStayWithTheBackend(t *testing.T) {
	var got []string
	base := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		got = append(got, r.URL.Host+"="+r.Header.Get("Authorization"))
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Request: r}, nil
	})
	transport := newAuthTransport(base, "http://memory.svc:8080/mcp", credentials.Forward("Bearer alice-token"))

	for _, target := range []string{"http://memory.svc:8080/mcp", "http://elsewhere.svc/mcp", "https://memory.svc:8080/mcp"} {
		req, err := http.NewRequest(http.MethodGet, target, nil)
		require.NoError(t, err)
		resp, err := transport.RoundTrip(req)
		require.NoError(t, err)
		_ = resp.Body.Close()
	}
	assert.Equal(t, []string{"memory.svc:8080=Bearer alice-token", "elsewhere.svc=", "memory.svc:8080="}, got)
}

func TestAuthFailuresAreNotSessionLoss(t *testing.T) {
	assert.False(t, sessionGone(fmt.Errorf("sending: %w: %w", errRejected, &RefusedError{StatusCode: 401})))
	assert.False(t, sessionGone(fmt.Errorf("sending: %w: %w", errRejected, &CredentialError{Err: errors.New("no secret")})))
	assert.True(t, sessionGone(fmt.Errorf("sending: %w", errRejected)))
}

func count(values []string, want string) int {
	n := 0
	for _, v := range values {
		if v == want {
			n++
		}
	}
	return n
}

// Over MCP 2026-07-28 there are no sessions to bind a token to: Passthrough
// calls share the one connection, each carrying its caller's token.
func TestAuth_PassthroughOverTheNewProtocolSharesOneConnection(t *testing.T) {
	backend := mcptest.NewStatelessBackend(t, "memory", "memory_search")
	backend.RequireBearer(map[string]string{
		"discovery-key": "gateway",
		"alice-token":   "alice",
		"bob-token":     "bob",
	})
	var seen requests
	backend.Guard(func(r *http.Request) int { seen.record(r); return 0 })
	secrets := newFakeSecrets()
	secrets.Set("memory-discovery", "key", "discovery-key")
	r := newTestRegistry(t, Options{Secrets: secrets})
	b := r.Ensure("memory", backend.URL, passthroughAuth())

	for _, caller := range []string{"alice", "bob", "alice"} {
		ctx := credentials.WithCaller(context.Background(), "Bearer "+caller+"-token")
		res, err := b.CallTool(ctx, &mcp.CallToolParams{Name: "memory_search"})
		require.NoError(t, err)
		assert.Equal(t, "memory/memory_search {} as "+caller, callText(t, res))
	}
	assert.Zero(t, b.callerSessions(), "no connection per caller")
	assert.Equal(t, []string{"Bearer alice-token", "Bearer bob-token", "Bearer alice-token"}, seen.authorizations("tools/call"))
	for _, authorization := range seen.authorizations("server/discover") {
		assert.Equal(t, "Bearer discovery-key", authorization, "the shared connection is opened with the discovery credential")
	}

	_, _, err := r.Discover(context.Background(), "memory")
	require.NoError(t, err, "catalog reads still use the discovery credential")

	ctx := credentials.WithCaller(context.Background(), "Bearer mallory-token")
	_, err = b.CallTool(ctx, &mcp.CallToolParams{Name: "memory_search"})
	var refused *RefusedError
	require.ErrorAs(t, err, &refused)
	assert.Equal(t, "the caller's token", refused.Credential)
	res, err := b.CallTool(credentials.WithCaller(context.Background(), "Bearer bob-token"), &mcp.CallToolParams{Name: "memory_search"})
	require.NoError(t, err, "a refused caller leaves the shared connection to the others")
	assert.Equal(t, "memory/memory_search {} as bob", callText(t, res))
}

// A backend that wants a token even to be discovered, and no discovery
// credential to give it: the shared connection cannot be opened, and calls get
// in on their callers' own connections.
func TestAuth_PassthroughWhenTheSharedConnectionCannotOpen(t *testing.T) {
	backend := mcptest.NewStatelessBackend(t, "memory", "memory_search")
	backend.RequireBearer(map[string]string{"alice-token": "alice"})
	r := newTestRegistry(t, Options{})
	b := r.Ensure("memory", backend.URL, &gatev1alpha1.BackendAuth{Type: gatev1alpha1.BackendAuthPassthrough})

	for range 3 {
		res, err := b.CallTool(credentials.WithCaller(context.Background(), "Bearer alice-token"), &mcp.CallToolParams{Name: "memory_search"})
		require.NoError(t, err)
		assert.Equal(t, "memory/memory_search {} as alice", callText(t, res))
	}
	assert.Equal(t, 1, b.callerSessions())
	_, _, err := r.Discover(context.Background(), "memory")
	assert.Error(t, err)
}
