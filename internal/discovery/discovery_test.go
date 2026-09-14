package discovery

import (
	"context"
	"encoding/json"
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

	assert.Equal(t, NewCatalog([]*mcp.Tool{a, b}).Revision, NewCatalog([]*mcp.Tool{b, a}).Revision)
	assert.Equal(t, []string{"a", "b"}, NewCatalog([]*mcp.Tool{b, a}).Names())
}

// A changed description is a changed catalog. Agents choose tools by their
// descriptions, so the gateway has to pass the new one on.
func TestCatalog_RevisionSeesDescriptionChanges(t *testing.T) {
	before := NewCatalog([]*mcp.Tool{{Name: "a", Description: "old"}})
	after := NewCatalog([]*mcp.Tool{{Name: "a", Description: "new"}})
	assert.NotEqual(t, before.Revision, after.Revision)
}

func TestCatalog_DuplicateNamesKeepTheFirst(t *testing.T) {
	c := NewCatalog([]*mcp.Tool{{Name: "a", Description: "first"}, {Name: "a", Description: "second"}})
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
	r.Ensure("artifact", backend.URL)

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
	r.Ensure("artifact", backend.URL)

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
	r.Ensure("artifact", backend.URL)
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
	r.Ensure("artifact", old.URL)
	_, _, err := r.Discover(context.Background(), "artifact")
	require.NoError(t, err)

	r.Ensure("artifact", replacement.URL)
	_, _, ok := r.Catalog("artifact")
	assert.False(t, ok, "the old catalog must be gone the moment the endpoint changes")

	catalog, _, err := r.Discover(context.Background(), "artifact")
	require.NoError(t, err)
	assert.Equal(t, []string{"other_tool"}, catalog.Names())
}

func TestRegistry_ForgetClosesTheBackend(t *testing.T) {
	backend := mcptest.NewBackend(t, "artifact", "artifact_get")
	r := newTestRegistry(t, Options{})
	b := r.Ensure("artifact", backend.URL)
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
	b := r.Ensure("artifact", backend.URL)

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
	b := r.Ensure("artifact", backend.URL)

	_, err := b.CallTool(context.Background(), &mcp.CallToolParams{Name: "artifact_get"})
	require.NoError(t, err)

	backend.Restart("artifact_get")

	res, err := b.CallTool(context.Background(), &mcp.CallToolParams{Name: "artifact_get"})
	require.NoError(t, err)
	assert.False(t, res.IsError)
}

func TestRegistry_BackendAnnouncementsReachOnToolsChanged(t *testing.T) {
	backend := mcptest.NewBackend(t, "artifact", "artifact_get")
	announced := make(chan string, 4)
	r := newTestRegistry(t, Options{OnToolsChanged: func(server string) { announced <- server }})
	r.Ensure("artifact", backend.URL)
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
