package discovery

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"forgejo.metapatrol.local/platform/mcp-gate/internal/mcptest"
)

func TestCatalog_RevisionIsOrderIndependent(t *testing.T) {
	a := &mcp.Tool{Name: "a", InputSchema: json.RawMessage(`{"type":"object"}`)}
	b := &mcp.Tool{Name: "b", InputSchema: json.RawMessage(`{"type":"object"}`)}

	assert.Equal(t, NewCatalog([]*mcp.Tool{a, b}, "").Revision, NewCatalog([]*mcp.Tool{b, a}, "").Revision)
	assert.Equal(t, []string{"a", "b"}, NewCatalog([]*mcp.Tool{b, a}, "").Names())
}

// A changed description is a changed catalog. Agents choose tools by their
// descriptions, so the gateway has to pass the new one on.
func TestCatalog_RevisionSeesDescriptionChanges(t *testing.T) {
	before := NewCatalog([]*mcp.Tool{{Name: "a", Description: "old"}}, "")
	after := NewCatalog([]*mcp.Tool{{Name: "a", Description: "new"}}, "")
	assert.NotEqual(t, before.Revision, after.Revision)
}

// Instructions are part of what a backend offers. A redeploy that only changes
// them still has to reach agents.
func TestCatalog_RevisionSeesInstructionChanges(t *testing.T) {
	tools := []*mcp.Tool{{Name: "a"}}
	assert.NotEqual(t, NewCatalog(tools, "old").Revision, NewCatalog(tools, "new").Revision)
	assert.Equal(t, NewCatalog(tools, "same").Revision, NewCatalog(tools, "same").Revision)
}

func TestRegistry_DiscoverReadsTheInstructions(t *testing.T) {
	backend := mcptest.NewBackendWithInstructions(t, "source-intelligence", "Search before you read.", "code.search")
	r := newTestRegistry(t, Options{})
	r.Ensure("source-intelligence", backend.URL, nil)

	catalog, _, err := r.Discover(context.Background(), "source-intelligence")
	require.NoError(t, err)
	assert.Equal(t, "Search before you read.", catalog.Instructions)
	assert.Equal(t, []string{"code.search"}, catalog.Names())
}

func TestCatalog_DuplicateNamesKeepTheFirst(t *testing.T) {
	c := NewCatalog([]*mcp.Tool{{Name: "a", Description: "first"}, {Name: "a", Description: "second"}}, "")
	tool, ok := c.Tool("a")
	require.True(t, ok)
	assert.Equal(t, "first", tool.Description)
	assert.Equal(t, 1, c.Len())
}

func newTestRegistry(t *testing.T, opts Options) *Registry {
	t.Helper()
	r := NewRegistry(opts)
	t.Cleanup(r.Close)
	return r
}

func TestRegistry_DiscoverReadsTheCatalog(t *testing.T) {
	backend := mcptest.NewBackend(t, "artifact", "artifact_get", "artifact_search")
	r := newTestRegistry(t, Options{})
	r.Ensure("artifact", backend.URL, nil)

	catalog, changed, err := r.Discover(context.Background(), "artifact")
	require.NoError(t, err)
	assert.True(t, changed, "the first catalog is a change")
	assert.Equal(t, []string{"artifact_get", "artifact_search"}, catalog.Names())

	again, changed, err := r.Discover(context.Background(), "artifact")
	require.NoError(t, err)
	assert.False(t, changed)
	assert.Same(t, catalog, again, "an unchanged catalog must keep its identity")
}

func TestRegistry_DiscoverNoticesChanges(t *testing.T) {
	backend := mcptest.NewBackend(t, "artifact", "artifact_get")
	r := newTestRegistry(t, Options{})
	r.Ensure("artifact", backend.URL, nil)

	first, _, err := r.Discover(context.Background(), "artifact")
	require.NoError(t, err)

	backend.AddTool("artifact_publish")
	second, changed, err := r.Discover(context.Background(), "artifact")
	require.NoError(t, err)
	assert.True(t, changed)
	assert.NotEqual(t, first.Revision, second.Revision)
}

// A backend that is down keeps its last catalog, and says how long it has been
// failing. Whether a stale catalog may still be served is the caller's call.
func TestRegistry_FailureKeepsTheLastCatalog(t *testing.T) {
	backend := mcptest.NewBackend(t, "artifact", "artifact_get")
	r := newTestRegistry(t, Options{ConnectTimeout: time.Second})
	r.Ensure("artifact", backend.URL, nil)
	_, _, err := r.Discover(context.Background(), "artifact")
	require.NoError(t, err)

	backend.Close()
	_, _, err = r.Discover(context.Background(), "artifact")
	require.Error(t, err)

	catalog, health, ok := r.Catalog("artifact")
	require.True(t, ok)
	assert.Equal(t, []string{"artifact_get"}, catalog.Names())
	assert.Equal(t, 1, health.Failures)
	assert.Error(t, health.LastError)
	assert.False(t, health.LastSuccess.IsZero())
}

// A new endpoint is a new backend. Its predecessor's catalog must not be served
// for it.
func TestRegistry_EndpointChangeDiscardsTheCatalog(t *testing.T) {
	old := mcptest.NewBackend(t, "artifact", "artifact_get")
	replacement := mcptest.NewBackend(t, "artifact", "other_tool")
	r := newTestRegistry(t, Options{})
	r.Ensure("artifact", old.URL, nil)
	_, _, err := r.Discover(context.Background(), "artifact")
	require.NoError(t, err)

	r.Ensure("artifact", replacement.URL, nil)
	_, _, ok := r.Catalog("artifact")
	assert.False(t, ok, "the old catalog must be gone the moment the endpoint changes")

	catalog, _, err := r.Discover(context.Background(), "artifact")
	require.NoError(t, err)
	assert.Equal(t, []string{"other_tool"}, catalog.Names())
}

func TestRegistry_ForgetClosesTheBackend(t *testing.T) {
	backend := mcptest.NewBackend(t, "artifact", "artifact_get")
	r := newTestRegistry(t, Options{})
	b := r.Ensure("artifact", backend.URL, nil)
	_, _, err := r.Discover(context.Background(), "artifact")
	require.NoError(t, err)

	r.Forget("artifact")
	_, ok := r.Backend("artifact")
	assert.False(t, ok)
	_, err = b.CallTool(context.Background(), &mcp.CallToolParams{Name: "artifact_get"})
	assert.ErrorIs(t, err, ErrClosed)
	_, _, err = r.Discover(context.Background(), "artifact")
	assert.ErrorIs(t, err, ErrClosed)
}

func TestBackend_CallTool(t *testing.T) {
	backend := mcptest.NewBackend(t, "artifact", "artifact_get")
	r := newTestRegistry(t, Options{})
	b := r.Ensure("artifact", backend.URL, nil)

	res, err := b.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "artifact_get",
		Arguments: json.RawMessage(`{"id":"42"}`),
	})
	require.NoError(t, err)
	require.Len(t, res.Content, 1)
	assert.Equal(t, `artifact/artifact_get {"id":"42"}`, res.Content[0].(*mcp.TextContent).Text)
}

// A backend restarted behind the same address no longer knows the session. The
// call must go through on a new one rather than fail until something notices.
func TestBackend_ReconnectsAfterTheBackendRestarts(t *testing.T) {
	backend := mcptest.NewBackend(t, "artifact", "artifact_get")
	r := newTestRegistry(t, Options{})
	b := r.Ensure("artifact", backend.URL, nil)

	_, err := b.CallTool(context.Background(), &mcp.CallToolParams{Name: "artifact_get"})
	require.NoError(t, err)

	backend.Restart("artifact_get")

	res, err := b.CallTool(context.Background(), &mcp.CallToolParams{Name: "artifact_get"})
	require.NoError(t, err)
	assert.False(t, res.IsError)
}

// stallingBackend wraps a real backend and holds every request for one of the
// given methods open without answering, the way the artifacts backend held
// subscriptions/listen. The held requests are released when the test ends.
func stallingBackend(t *testing.T, backend *mcptest.Backend, methods ...string) *httptest.Server {
	t.Helper()
	release := make(chan struct{})
	target, err := url.Parse(backend.URL)
	require.NoError(t, err)
	forward := httputil.NewSingleHostReverseProxy(target)
	forward.FlushInterval = -1

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			body, _ := io.ReadAll(r.Body)
			var msg struct {
				Method string `json:"method"`
			}
			_ = json.Unmarshal(body, &msg)
			if slices.Contains(methods, msg.Method) {
				<-release
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
			r.ContentLength = int64(len(body))
		}
		forward.ServeHTTP(w, r)
	}))
	t.Cleanup(func() {
		close(release)
		srv.CloseClientConnections()
		srv.Close()
	})
	return srv
}

// The protocol is the SDK's to negotiate: MCP 2026-07-28 with a backend that
// speaks it, the newest session-based version with one that does not.
func TestBackend_NegotiatesTheProtocol(t *testing.T) {
	for _, tc := range []struct {
		name    string
		backend *mcptest.Backend
		want    string
	}{
		{"stateless", mcptest.NewStatelessBackend(t, "artifacts", "artifact_get"), "2026-07-28"},
		{"session-based", mcptest.NewBackend(t, "memory", "memory_search"), "2025-11-25"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newTestRegistry(t, Options{})
			b := r.Ensure(tc.backend.Name, tc.backend.URL, nil)
			catalog, _, err := r.Discover(context.Background(), tc.backend.Name)
			require.NoError(t, err)
			assert.Len(t, catalog.Names(), 1)
			session, err := b.shared.connect(context.Background())
			require.NoError(t, err)
			assert.Equal(t, tc.want, session.InitializeResult().ProtocolVersion)
		})
	}
}

// The regression this guards: with a list_changed handler, the SDK opens
// subscriptions/listen inside Connect and waits for the answer with no
// deadline, and the artifacts backend accepts that request and never answers.
// Notifications have a connection of their own, so a backend doing that costs
// neither a catalog read nor a tool call anything.
func TestBackend_AListenThatNeverAnswersBlocksNothing(t *testing.T) {
	backend := mcptest.NewStatelessBackend(t, "artifacts", "artifact_get")
	stalling := stallingBackend(t, backend, "subscriptions/listen")
	r := newTestRegistry(t, Options{
		ConnectTimeout: 2 * time.Second,
		OnToolsChanged: func(string) {},
	})
	b := r.Ensure("artifacts", stalling.URL, nil)

	start := time.Now()
	catalog, _, err := r.Discover(context.Background(), "artifacts")
	require.NoError(t, err)
	assert.Equal(t, []string{"artifact_get"}, catalog.Names())
	_, err = b.CallTool(context.Background(), &mcp.CallToolParams{Name: "artifact_get"})
	require.NoError(t, err)
	assert.Less(t, time.Since(start), time.Second)
}

// A backend that never answers the handshake costs a caller the connect
// timeout — not the goroutine it runs on — and a later attempt, once the backend
// answers again, gets through.
func TestBackend_AStalledHandshakeIsAbandonedAtTheTimeout(t *testing.T) {
	backend := mcptest.NewBackend(t, "artifacts", "artifact_get")
	var stall atomic.Bool
	stall.Store(true)
	release := make(chan struct{})
	target, err := url.Parse(backend.URL)
	require.NoError(t, err)
	forward := httputil.NewSingleHostReverseProxy(target)
	forward.FlushInterval = -1
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if stall.Load() {
			<-release
			return
		}
		forward.ServeHTTP(w, r)
	}))
	t.Cleanup(func() {
		close(release)
		srv.CloseClientConnections()
		srv.Close()
	})

	r := newTestRegistry(t, Options{ConnectTimeout: 300 * time.Millisecond})
	r.Ensure("artifacts", srv.URL, nil)

	start := time.Now()
	_, _, err = r.Discover(context.Background(), "artifacts")
	require.Error(t, err)
	assert.Less(t, time.Since(start), 2*time.Second, "the caller must get its answer at the timeout")

	stall.Store(false)
	require.Eventually(t, func() bool {
		_, _, err := r.Discover(context.Background(), "artifacts")
		return err == nil
	}, 5*time.Second, 50*time.Millisecond, "a recovered backend must be reachable again")
}

func TestRegistry_BackendAnnouncementsReachOnToolsChanged(t *testing.T) {
	backend := mcptest.NewBackend(t, "artifact", "artifact_get")
	announced := make(chan string, 4)
	r := newTestRegistry(t, Options{OnToolsChanged: func(server string) { announced <- server }})
	r.Ensure("artifact", backend.URL, nil)
	_, _, err := r.Discover(context.Background(), "artifact")
	require.NoError(t, err)

	// The standalone SSE stream that carries the notification is opened after
	// the handshake; give it a moment before changing anything.
	require.Eventually(t, func() bool {
		backend.AddTool("artifact_publish")
		select {
		case server := <-announced:
			return server == "artifact"
		case <-time.After(200 * time.Millisecond):
			return false
		}
	}, 5*time.Second, 10*time.Millisecond)
}

// A backend speaking MCP 2026-07-28 announces changes over subscriptions/listen.
func TestRegistry_AnnouncementsOverSubscriptionsListen(t *testing.T) {
	backend := mcptest.NewStatelessBackend(t, "artifacts", "artifact_get")
	announced := make(chan string, 4)
	r := newTestRegistry(t, Options{OnToolsChanged: func(server string) { announced <- server }})
	r.Ensure("artifacts", backend.URL, nil)
	_, _, err := r.Discover(context.Background(), "artifacts")
	require.NoError(t, err)

	requireAnnounced(t, backend, announced, "artifacts", "artifact_publish")
}

// The listener connects again when its connection ends, so a backend that
// restarted is heard from afterwards too.
func TestRegistry_AnnouncementsSurviveABackendRestart(t *testing.T) {
	for _, tc := range []struct {
		name    string
		backend *mcptest.Backend
	}{
		{"stateless", mcptest.NewStatelessBackend(t, "artifacts", "artifact_get")},
		{"session-based", mcptest.NewBackend(t, "artifacts", "artifact_get")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			announced := make(chan string, 16)
			r := newTestRegistry(t, Options{OnToolsChanged: func(server string) { announced <- server }})
			r.Ensure("artifacts", tc.backend.URL, nil)
			requireAnnounced(t, tc.backend, announced, "artifacts", "artifact_publish")

			tc.backend.Restart("artifact_get")
			requireAnnounced(t, tc.backend, announced, "artifacts", "artifact_delete")
		})
	}
}

func TestBackend_CloseStopsTheListener(t *testing.T) {
	backend := mcptest.NewStatelessBackend(t, "artifacts", "artifact_get")
	r := newTestRegistry(t, Options{OnToolsChanged: func(string) {}})
	b := r.Ensure("artifacts", backend.URL, nil)
	time.Sleep(100 * time.Millisecond)

	require.NoError(t, b.Close())
	select {
	case <-b.listening:
	case <-time.After(5 * time.Second):
		t.Fatal("the listener is still running")
	}
}

// requireAnnounced changes the backend's tools until the registry passes the
// announcement on. The listener connects in the background, so the first
// changes may happen before it hears anything.
func requireAnnounced(t *testing.T, backend *mcptest.Backend, announced chan string, server, tool string) {
	t.Helper()
	require.Eventually(t, func() bool {
		backend.AddTool(tool)
		backend.RemoveTool(tool)
		for {
			select {
			case got := <-announced:
				if got == server {
					return true
				}
			case <-time.After(200 * time.Millisecond):
				return false
			}
		}
	}, 20*time.Second, 10*time.Millisecond)
}
