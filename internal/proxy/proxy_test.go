package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tokenEndpoint issues numbered tokens to one client, and counts how many.
type tokenEndpoint struct {
	*httptest.Server
	issued    atomic.Int64
	expiresIn int
}

func newTokenEndpoint(t *testing.T) *tokenEndpoint {
	t.Helper()
	te := &tokenEndpoint{expiresIn: 3600}
	te.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		id, secret, ok := r.BasicAuth()
		if !ok {
			id, secret = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
		}
		if r.PostForm.Get("grant_type") != "client_credentials" || id != "coding-agent" || secret != "s3cret" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"error":"invalid_client"}`)
			return
		}
		n := te.issued.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": fmt.Sprintf("token-%d", n),
			"token_type":   "bearer",
			"expires_in":   te.expiresIn,
		})
	}))
	t.Cleanup(te.Close)
	return te
}

func newProxy(t *testing.T, gatewayURL string, te *tokenEndpoint, edit func(*Config)) *httptest.Server {
	t.Helper()
	cfg := Config{GatewayURL: gatewayURL, TokenURL: te.URL, ClientID: "coding-agent", ClientSecret: "s3cret"}
	if edit != nil {
		edit(&cfg)
	}
	p, err := New(cfg)
	require.NoError(t, err)
	srv := httptest.NewServer(p)
	t.Cleanup(srv.Close)
	return srv
}

// A real MCP server that demands a valid token, standing in for the gateway.
// The proxy has to carry a whole session through it — handshake, session ID,
// streamed results — without knowing any of that is happening.
func TestProxy_CarriesAnMCPSessionToAProtectedServer(t *testing.T) {
	te := newTokenEndpoint(t)
	server := mcp.NewServer(&mcp.Implementation{Name: "gateway", Version: "test"}, nil)
	server.AddTool(&mcp.Tool{Name: "whoami", InputSchema: json.RawMessage(`{"type":"object"}`)},
		func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: req.Extra.TokenInfo.UserID}}}, nil
		})
	verifier := func(_ context.Context, token string, _ *http.Request) (*sdkauth.TokenInfo, error) {
		if !strings.HasPrefix(token, "token-") {
			return nil, sdkauth.ErrInvalidToken
		}
		return &sdkauth.TokenInfo{UserID: "coding-agent:" + token, Expiration: time.Now().Add(time.Hour)}, nil
	}
	gateway := httptest.NewServer(sdkauth.RequireBearerToken(verifier, nil)(
		mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)))
	t.Cleanup(gateway.Close)

	proxy := newProxy(t, gateway.URL+"/mcp", te, nil)

	client := mcp.NewClient(&mcp.Implementation{Name: "agent", Version: "test"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: proxy.URL + "/mcp"}, nil)
	require.NoError(t, err, "an agent without any credentials must get through")
	defer func() { _ = session.Close() }()

	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "whoami"})
	require.NoError(t, err)
	assert.Equal(t, "coding-agent:token-1", res.Content[0].(*mcp.TextContent).Text)
	assert.Equal(t, int64(1), te.issued.Load(), "the token must be reused across requests")
}

// A gateway that refuses a token which by its own exp is still valid — revoked,
// say. The proxy fetches a new token and replays the request, body included,
// and the agent never sees the 401.
func TestProxy_RetriesARefusedTokenOnceWithAFreshOne(t *testing.T) {
	te := newTokenEndpoint(t)
	var bodies []string
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(body))
		if r.Header.Get("Authorization") == "Bearer token-1" {
			w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="https://x"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = io.WriteString(w, "ok")
	}))
	t.Cleanup(gateway.Close)
	proxy := newProxy(t, gateway.URL+"/mcp", te, nil)

	resp, err := http.Post(proxy.URL+"/mcp", "application/json", strings.NewReader(`{"jsonrpc":"2.0"}`))
	require.NoError(t, err)
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "ok", string(body))
	assert.Equal(t, []string{`{"jsonrpc":"2.0"}`, `{"jsonrpc":"2.0"}`}, bodies)
	assert.Equal(t, int64(2), te.issued.Load())
}

// Credentials the gateway keeps refusing are the proxy's problem, not the
// agent's. Passing on the 401 and its challenge would start an OAuth flow in an
// agent that was set up precisely to have none.
func TestProxy_PersistentRefusalIsNotPassedOnAsAChallenge(t *testing.T) {
	te := newTokenEndpoint(t)
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="https://x"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(gateway.Close)
	proxy := newProxy(t, gateway.URL+"/mcp", te, nil)

	resp, err := http.Post(proxy.URL+"/mcp", "application/json", strings.NewReader(`{}`))
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusBadGateway, resp.StatusCode)
	assert.Empty(t, resp.Header.Get("WWW-Authenticate"))
}

func TestProxy_BadClientCredentials(t *testing.T) {
	te := newTokenEndpoint(t)
	var reached atomic.Bool
	gateway := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached.Store(true) }))
	t.Cleanup(gateway.Close)
	proxy := newProxy(t, gateway.URL+"/mcp", te, func(c *Config) { c.ClientSecret = "wrong" })

	resp, err := http.Post(proxy.URL+"/mcp", "application/json", strings.NewReader(`{}`))
	require.NoError(t, err)
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusBadGateway, resp.StatusCode)
	assert.Contains(t, string(body), "access token")
	assert.False(t, reached.Load(), "nothing goes to the gateway without a token")
}

func TestProxy_ReplacesTheAgentsOwnCredentials(t *testing.T) {
	te := newTokenEndpoint(t)
	var got http.Header
	gateway := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = r.Header.Clone() }))
	t.Cleanup(gateway.Close)
	proxy := newProxy(t, gateway.URL+"/mcp", te, nil)

	req, _ := http.NewRequest(http.MethodPost, proxy.URL+"/anything", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer agent-supplied")
	req.Header.Set("Cookie", "session=abc")
	req.Header.Set("Mcp-Session-Id", "s-1")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()

	assert.Equal(t, "Bearer token-1", got.Get("Authorization"))
	assert.Empty(t, got.Get("Cookie"))
	assert.Equal(t, "s-1", got.Get("Mcp-Session-Id"), "MCP headers pass through untouched")
}

func TestProxy_RefreshesBeforeExpiry(t *testing.T) {
	te := newTokenEndpoint(t)
	te.expiresIn = 10 // inside the refresh margin: every request needs a new one
	gateway := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(gateway.Close)
	proxy := newProxy(t, gateway.URL+"/mcp", te, nil)

	for range 3 {
		resp, err := http.Post(proxy.URL+"/mcp", "application/json", strings.NewReader(`{}`))
		require.NoError(t, err)
		_ = resp.Body.Close()
	}
	assert.Equal(t, int64(3), te.issued.Load())
}

// DNS rebinding: a page on evil.example resolving to 127.0.0.1 still sends its
// own name in the Host header.
func TestProxy_RefusesForeignHostNames(t *testing.T) {
	te := newTokenEndpoint(t)
	gateway := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(gateway.Close)
	proxy := newProxy(t, gateway.URL+"/mcp", te, func(c *Config) { c.AllowedHosts = []string{"10.0.0.7"} })

	for host, want := range map[string]int{
		"evil.example:8765": http.StatusForbidden,
		"localhost:8765":    http.StatusOK,
		"127.0.0.1:8765":    http.StatusOK,
		"[::1]:8765":        http.StatusOK,
		"10.0.0.7:8765":     http.StatusOK,
	} {
		req, _ := http.NewRequest(http.MethodPost, proxy.URL+"/mcp", strings.NewReader(`{}`))
		req.Host = host
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		_ = resp.Body.Close()
		assert.Equal(t, want, resp.StatusCode, host)
	}
	assert.Equal(t, int64(1), te.issued.Load())
}

// A browser posting cross-site says so in Sec-Fetch-Site.
func TestProxy_RefusesCrossOriginBrowserRequests(t *testing.T) {
	te := newTokenEndpoint(t)
	gateway := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(gateway.Close)
	proxy := newProxy(t, gateway.URL+"/mcp", te, nil)

	req, _ := http.NewRequest(http.MethodPost, proxy.URL+"/mcp", strings.NewReader(`{}`))
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	assert.Zero(t, te.issued.Load())
}

func TestNew_Validation(t *testing.T) {
	_, err := New(Config{GatewayURL: "/relative", TokenURL: "http://t", ClientID: "a", ClientSecret: "b"})
	assert.Error(t, err)
	_, err = New(Config{GatewayURL: "http://gw/mcp", TokenURL: "http://t", ClientID: "a"})
	assert.Error(t, err)
}
