package gateway

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	mcpv1alpha1 "forgejo.metapatrol.local/platform/mcp-gateway/api/v1alpha1"
)

var sessionBasedVersions = []string{"2025-11-25", "2025-06-18", "2025-03-26"}

// connectAs connects a client that speaks the given version, and hears about
// changes on changed.
func (e *env) connectAs(token, version string, changed chan<- struct{}) *mcp.ClientSession {
	e.t.Helper()
	client := mcp.NewClient(&mcp.Implementation{Name: "agent", Version: version}, &mcp.ClientOptions{
		ToolListChangedHandler: func(context.Context, *mcp.ToolListChangedRequest) {
			if changed != nil {
				changed <- struct{}{}
			}
		},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint:   e.url,
		HTTPClient: &http.Client{Transport: bearer(token)},
	}, &mcp.ClientSessionOptions{ProtocolVersion: version})
	require.NoError(e.t, err, version)
	e.t.Cleanup(func() { _ = session.Close() })
	return session
}

// The Streamable HTTP versions before 2026-07-28 are served without sessions:
// the tools, the instructions and every call, authorised every time.
func TestSessionBasedVersionsAreServedWithoutSessions(t *testing.T) {
	for _, version := range sessionBasedVersions {
		t.Run(version, func(t *testing.T) {
			e := newEnv(t, Options{})
			session := e.connectAs("token-reviewer", version, nil)

			assert.Equal(t, version, session.InitializeResult().ProtocolVersion)
			assert.Empty(t, session.ID(), "no session is issued")
			assert.Contains(t, session.InitializeResult().Instructions, "Publish with a title.")
			assert.Equal(t, []string{"artifact_get", "artifact_publish", "artifact_search"}, toolNames(t, session))

			res, err := call(session, "artifact_publish")
			require.NoError(t, err)
			assert.Equal(t, `artifact/artifact_publish {"q":"x"}`, text(res))
			_, err = call(session, "source_search")
			assert.Error(t, err, "outside the grants")

			e.grants = nil
			e.apply()
			require.Eventually(t, func() bool {
				_, err := call(session, "artifact_publish")
				return err != nil
			}, 5*time.Second, 20*time.Millisecond, "a revoked grant is refused on the next call")
		})
	}
}

// An agent of a session-based version hears that its tools changed on its GET
// stream — without a session, on whichever replica the stream landed.
func TestSessionBasedVersionsHearAboutChanges(t *testing.T) {
	for _, version := range sessionBasedVersions {
		t.Run(version, func(t *testing.T) {
			e := newEnv(t, Options{})
			changed := make(chan struct{}, 8)
			session := e.connectAs("token-coding-agent", version, changed)
			require.Contains(t, toolNames(t, session), "artifact_get")

			// The client opens its GET stream in the background; a change
			// made before it is open would go unheard.
			require.Eventually(t, func() bool { return e.gateway.streamsOf("coding-agent") > 0 },
				5*time.Second, 10*time.Millisecond)

			e.grants = []mcpv1alpha1.MCPGrant{grant("reviewer", "reviewer", "artifact-authoring")}
			e.apply()
			select {
			case <-changed:
			case <-time.After(5 * time.Second):
				t.Fatal("the agent was not told that its tools changed")
			}
			assert.Empty(t, toolNames(t, session))

			require.NoError(t, session.Close())
			require.Eventually(t, func() bool { return e.gateway.streamsOf("coding-agent") == 0 },
				5*time.Second, 10*time.Millisecond, "a closed stream is let go")
		})
	}
}

// What the GET stream answers, request by request.
func TestChangeStream(t *testing.T) {
	e := newEnv(t, Options{})
	get := func(token, version, accept string) *http.Response {
		t.Helper()
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, e.url, nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if version != "" {
			req.Header.Set("MCP-Protocol-Version", version)
		}
		req.Header.Set("Accept", accept)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		t.Cleanup(func() { _ = resp.Body.Close() })
		return resp
	}

	assert.Equal(t, http.StatusUnauthorized, get("", "", "text/event-stream").StatusCode, "the token first")
	assert.Equal(t, http.StatusMethodNotAllowed, get("token-reviewer", ProtocolVersion, "text/event-stream").StatusCode,
		"2026-07-28 listens with subscriptions/listen")
	assert.Equal(t, http.StatusNotAcceptable, get("token-reviewer", "", "application/json").StatusCode)
	assert.Equal(t, http.StatusNotAcceptable, get("token-reviewer", "", "*/*").StatusCode,
		"a plain GET is not held open: an MCP client names the stream")

	resp := get("token-reviewer", "2025-06-18", "text/event-stream")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))
	assert.Empty(t, resp.Header.Get("Mcp-Session-Id"))

	require.Eventually(t, func() bool { return e.gateway.streamsOf("reviewer") == 1 }, 5*time.Second, 10*time.Millisecond)
	e.grants = nil
	e.apply()
	lines := bufio.NewScanner(resp.Body)
	var event []string
	for lines.Scan() && lines.Text() != "" {
		event = append(event, lines.Text())
	}
	assert.Equal(t, []string{"event: message", `data: {"jsonrpc":"2.0","method":"notifications/tools/list_changed"}`}, event)
}

// What a request names decides whether it is answered: a version the gateway
// speaks, or none, as 2025-03-26 clients and every initialize before
// 2026-07-28 send.
func TestProtocolVersionHeader(t *testing.T) {
	e := newEnv(t, Options{})
	send := func(method, version string, params map[string]any) (int, string) {
		t.Helper()
		raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
		req, _ := http.NewRequest(http.MethodPost, e.url, bytes.NewReader(raw))
		req.Header.Set("Authorization", "Bearer token-reviewer")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		if version != "" {
			req.Header.Set("MCP-Protocol-Version", version)
		}
		if version == ProtocolVersion {
			req.Header.Set("Mcp-Method", method)
		}
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(body)
	}
	initialize := func(version string) map[string]any {
		return map[string]any{"protocolVersion": version, "capabilities": map[string]any{},
			"clientInfo": map[string]any{"name": "agent", "version": "test"}}
	}

	for _, version := range []string{"2024-11-05", "2024-10-07", "1999-01-01"} {
		code, body := send("tools/list", version, map[string]any{})
		assert.Equal(t, http.StatusBadRequest, code, version)
		assert.Contains(t, body, "supported versions: "+strings.Join(ProtocolVersions, ", "), version)
	}
	assert.Zero(t, e.artifact.Calls.Load())

	code, body := send("initialize", "", initialize("2025-03-26"))
	assert.Equal(t, http.StatusOK, code)
	assert.Contains(t, body, `"protocolVersion":"2025-03-26"`)

	// Asked for a version it does not speak, initialize answers with the
	// newest session-based one, as the protocol has it negotiate.
	code, body = send("initialize", "", initialize("2024-11-05"))
	assert.Equal(t, http.StatusOK, code)
	assert.Contains(t, body, `"protocolVersion":"2025-11-25"`)

	code, body = send("tools/list", "", map[string]any{})
	assert.Equal(t, http.StatusOK, code)
	assert.Contains(t, body, "artifact_publish")

	meta := map[string]any{"_meta": map[string]any{
		"io.modelcontextprotocol/protocolVersion":    ProtocolVersion,
		"io.modelcontextprotocol/clientInfo":         map[string]any{"name": "agent", "version": "test"},
		"io.modelcontextprotocol/clientCapabilities": map[string]any{},
	}}
	code, body = send("tools/list", ProtocolVersion, meta)
	assert.Equal(t, http.StatusOK, code)
	assert.Contains(t, body, "artifact_publish")

	// 2026-07-28 has no initialize; server/discover takes its place.
	code, body = send("initialize", ProtocolVersion, meta)
	assert.Equal(t, http.StatusNotFound, code)
	assert.Contains(t, body, "method not found")
}

// An endpoint served at / answers only /. A client probing for OAuth metadata
// at the well-known path — LibreChat does, with a plain GET — gets a 404 at
// once, not the endpoint and its stream.
func TestEndpointAtTheRootServesOnlyTheRoot(t *testing.T) {
	e := newEnv(t, Options{})
	handler, err := e.gateway.Handler(HandlerOptions{Path: "/", Verifier: fakeVerifier})
	require.NoError(t, err)

	for _, path := range []string{"/.well-known/oauth-protected-resource", "/.well-known/oauth-protected-resource/", "/mcp", "/anything"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer token-reviewer")
		handler.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusNotFound, rec.Code, path)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{}`))
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code, "the endpoint itself")
}
