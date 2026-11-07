package discovery

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	mcpv1alpha1 "forgejo.metapatrol.local/platform/mcp-gateway/api/v1alpha1"
	"forgejo.metapatrol.local/platform/mcp-gateway/internal/mcptest"
)

func sseServer(name, url string, auth *mcpv1alpha1.BackendAuth) *mcpv1alpha1.MCPServer {
	return &mcpv1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec:       mcpv1alpha1.MCPServerSpec{Endpoint: url, Transport: mcpv1alpha1.TransportSSE, Auth: auth},
	}
}

// A backend that speaks only HTTP+SSE is discovered and called like any other.
func TestSSE_DiscoverAndCall(t *testing.T) {
	backend := mcptest.NewSSEBackend(t, "crawl", "crawl_page", "crawl_site")
	r := newTestRegistry(t, Options{})
	b := r.EnsureServer(sseServer("crawl", backend.URL, nil))
	assert.Equal(t, mcpv1alpha1.TransportSSE, b.Transport())

	catalog, _, err := r.Discover(context.Background(), "crawl")
	require.NoError(t, err)
	assert.Equal(t, []string{"crawl_page", "crawl_site"}, catalog.Names())

	res, err := b.CallTool(context.Background(), &mcp.CallToolParams{Name: "crawl_page", Arguments: map[string]any{"url": "x"}})
	require.NoError(t, err)
	assert.Equal(t, `crawl/crawl_page {"url":"x"}`, callText(t, res))
}

// The Crawl4AI case: a static token from a Secret, on the event stream and
// on every request posted.
func TestSSE_APIKey(t *testing.T) {
	backend := mcptest.NewSSEBackend(t, "crawl", "crawl_page")
	backend.RequireBearer(map[string]string{"k-123": "gateway"})
	secrets := newFakeSecrets()
	secrets.Set("crawl", "token", "k-123")
	r := newTestRegistry(t, Options{Secrets: secrets})
	b := r.EnsureServer(sseServer("crawl", backend.URL, &mcpv1alpha1.BackendAuth{
		Type:   mcpv1alpha1.BackendAuthAPIKey,
		APIKey: &mcpv1alpha1.APIKeyAuth{SecretRef: mcpv1alpha1.SecretKeyRef{Name: "crawl", Key: "token"}},
	}))

	// The SDK's SSE server does not hand the token's user to the tool, so
	// the result cannot name it; the backend refusing a call without the
	// key is what shows the key went along.
	res, err := b.CallTool(context.Background(), &mcp.CallToolParams{Name: "crawl_page"})
	require.NoError(t, err)
	assert.Equal(t, "crawl/crawl_page {}", callText(t, res))

	without := newTestRegistry(t, Options{}).EnsureServer(sseServer("crawl", backend.URL, nil))
	_, err = without.CallTool(context.Background(), &mcp.CallToolParams{Name: "crawl_page"})
	var refused *RefusedError
	require.ErrorAs(t, err, &refused)
	assert.Equal(t, http.StatusUnauthorized, refused.StatusCode)
}

func TestSSE_AnnouncementsReachOnToolsChanged(t *testing.T) {
	backend := mcptest.NewSSEBackend(t, "crawl", "crawl_page")
	announced := make(chan string, 4)
	r := newTestRegistry(t, Options{OnToolsChanged: func(server string) { announced <- server }})
	r.EnsureServer(sseServer("crawl", backend.URL, nil))
	_, _, err := r.Discover(context.Background(), "crawl")
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		backend.AddTool("crawl_site")
		select {
		case server := <-announced:
			return server == "crawl"
		case <-time.After(200 * time.Millisecond):
			return false
		}
	}, 5*time.Second, 10*time.Millisecond)
}

// Another transport is another backend, as another endpoint is: what the old
// one offered says nothing about the new one.
func TestSSE_ATransportChangeDiscardsTheCatalog(t *testing.T) {
	streamable := mcptest.NewBackend(t, "crawl", "crawl_page")
	r := newTestRegistry(t, Options{})
	r.Ensure("crawl", streamable.URL, nil)
	_, _, err := r.Discover(context.Background(), "crawl")
	require.NoError(t, err)

	b := r.EnsureServer(sseServer("crawl", streamable.URL, nil))
	assert.Equal(t, mcpv1alpha1.TransportSSE, b.Transport())
	_, _, ok := r.Catalog("crawl")
	assert.False(t, ok)

	same := r.EnsureServer(sseServer("crawl", streamable.URL, nil))
	assert.Same(t, b, same, "an unchanged server keeps its backend")
}
