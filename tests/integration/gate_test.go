package integration

import (
	"context"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gatev1alpha1 "forgejo.metapatrol.local/platform/mcp-gate/api/v1alpha1"
	"forgejo.metapatrol.local/platform/mcp-gate/internal/mcptest"
)

const (
	eventually = 20 * time.Second
	tick       = 100 * time.Millisecond
)

func create(t *testing.T, obj client.Object) {
	t.Helper()
	require.NoError(t, k8sClient.Create(context.Background(), obj))
	t.Cleanup(func() { _ = k8sClient.Delete(context.Background(), obj) })
}

func readyStatus(t *testing.T, obj client.Object) (metav1.ConditionStatus, string) {
	t.Helper()
	if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: obj.GetName()}, obj); err != nil {
		return "", err.Error()
	}
	var conditions []metav1.Condition
	switch o := obj.(type) {
	case *gatev1alpha1.MCPServer:
		conditions = o.Status.Conditions
	case *gatev1alpha1.MCPToolSet:
		conditions = o.Status.Conditions
	case *gatev1alpha1.MCPCapability:
		conditions = o.Status.Conditions
	case *gatev1alpha1.MCPGrant:
		conditions = o.Status.Conditions
	}
	c := meta.FindStatusCondition(conditions, gatev1alpha1.ConditionReady)
	if c == nil {
		return "", ""
	}
	return c.Status, c.Reason
}

func requireReady(t *testing.T, obj client.Object, want metav1.ConditionStatus, reason string) {
	t.Helper()
	require.Eventually(t, func() bool {
		status, got := readyStatus(t, obj)
		return status == want && (reason == "" || got == reason)
	}, eventually, tick, "%T %s never became Ready=%s (%s)", obj, obj.GetName(), want, reason)
}

func names(t *testing.T, session *mcp.ClientSession) []string {
	t.Helper()
	res, err := session.ListTools(context.Background(), nil)
	require.NoError(t, err)
	out := make([]string, 0, len(res.Tools))
	for _, tool := range res.Tools {
		out = append(out, tool.Name)
	}
	sort.Strings(out)
	return out
}

// The whole model end to end: manifests in the API server, statuses from the
// controller, and a gateway serving what the manifests say.
func TestGate(t *testing.T) {
	sourceIntelligence := mcptest.NewBackend(t, "source-intelligence", "source_search", "source_get", "symbol_search", "repository_search")
	artifact := mcptest.NewBackend(t, "artifact", "artifact_search", "artifact_get", "artifact_publish")

	startController(t)
	url, _ := startGateway(t)

	servers := []*gatev1alpha1.MCPServer{
		{ObjectMeta: metav1.ObjectMeta{Name: "source-intelligence"}, Spec: gatev1alpha1.MCPServerSpec{Endpoint: sourceIntelligence.URL}},
		{ObjectMeta: metav1.ObjectMeta{Name: "artifact"}, Spec: gatev1alpha1.MCPServerSpec{Endpoint: artifact.URL}},
	}
	for _, s := range servers {
		create(t, s)
	}
	analysis := &gatev1alpha1.MCPToolSet{
		ObjectMeta: metav1.ObjectMeta{Name: "source-intelligence-analysis"},
		Spec:       gatev1alpha1.MCPToolSetSpec{ServerRef: "source-intelligence", Tools: []string{"source_search", "source_get", "symbol_search"}},
	}
	artifactRead := &gatev1alpha1.MCPToolSet{
		ObjectMeta: metav1.ObjectMeta{Name: "artifact-read"},
		Spec:       gatev1alpha1.MCPToolSetSpec{ServerRef: "artifact", Tools: []string{"artifact_search", "artifact_get"}},
	}
	research := &gatev1alpha1.MCPCapability{
		ObjectMeta: metav1.ObjectMeta{Name: "code-research"},
		Spec:       gatev1alpha1.MCPCapabilitySpec{ToolSets: []string{"source-intelligence-analysis", "artifact-read"}},
	}
	grant := &gatev1alpha1.MCPGrant{
		ObjectMeta: metav1.ObjectMeta{Name: "coding-agent"},
		Spec:       gatev1alpha1.MCPGrantSpec{Subject: "coding-agent", Capabilities: []string{"code-research"}},
	}
	for _, obj := range []client.Object{analysis, artifactRead, research, grant} {
		create(t, obj)
	}

	t.Run("statuses settle", func(t *testing.T) {
		for _, s := range servers {
			requireReady(t, s, metav1.ConditionTrue, gatev1alpha1.ReasonDiscovered)
		}
		requireReady(t, analysis, metav1.ConditionTrue, gatev1alpha1.ReasonAvailable)
		requireReady(t, artifactRead, metav1.ConditionTrue, gatev1alpha1.ReasonAvailable)
		requireReady(t, research, metav1.ConditionTrue, gatev1alpha1.ReasonAvailable)
		requireReady(t, grant, metav1.ConditionTrue, gatev1alpha1.ReasonAvailable)

		var s gatev1alpha1.MCPServer
		require.NoError(t, k8sClient.Get(context.Background(), types.NamespacedName{Name: "source-intelligence"}, &s))
		assert.True(t, s.Status.Available)
		assert.Equal(t, 4, s.Status.DiscoveredTools)
		assert.True(t, strings.HasPrefix(s.Status.CatalogRevision, "sha256:"))
		require.NotNil(t, s.Spec.DiscoveryInterval, "the schema default applies")
		assert.Equal(t, time.Minute, s.Spec.DiscoveryInterval.Duration)
	})

	changed := make(chan struct{}, 16)
	session := connect(t, url, "coding-agent", &mcp.ClientOptions{
		ToolListChangedHandler: func(context.Context, *mcp.ToolListChangedRequest) { changed <- struct{}{} },
	})

	t.Run("the gateway serves the grant", func(t *testing.T) {
		require.Eventually(t, func() bool {
			return slices.Equal(names(t, session),
				[]string{"artifact_get", "artifact_search", "source_get", "source_search", "symbol_search"})
		}, eventually, tick)

		res, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "symbol_search", Arguments: map[string]any{}})
		require.NoError(t, err)
		assert.Equal(t, "source-intelligence/symbol_search {}", res.Content[0].(*mcp.TextContent).Text)

		_, err = session.CallTool(context.Background(), &mcp.CallToolParams{Name: "artifact_publish"})
		assert.Error(t, err)
		assert.Empty(t, names(t, connect(t, url, "somebody-else", nil)))
	})

	// A tool the backend stops exposing: the backend announces it, the tool
	// set reports it, the gateway drops the tool and keeps the rest.
	t.Run("a tool disappears from its backend", func(t *testing.T) {
		sourceIntelligence.RemoveTool("symbol_search")

		requireReady(t, analysis, metav1.ConditionFalse, gatev1alpha1.ReasonMissingTools)
		requireReady(t, research, metav1.ConditionFalse, gatev1alpha1.ReasonToolSetNotReady)
		requireReady(t, grant, metav1.ConditionFalse, gatev1alpha1.ReasonCapabilityNotReady)
		require.NoError(t, k8sClient.Get(context.Background(), types.NamespacedName{Name: analysis.Name}, analysis))
		assert.Equal(t, []string{"symbol_search"}, analysis.Status.MissingTools)

		require.Eventually(t, func() bool {
			return !slices.Contains(names(t, session), "symbol_search")
		}, eventually, tick)
		assert.Contains(t, names(t, session), "source_search")

		sourceIntelligence.AddTool("symbol_search")
		requireReady(t, grant, metav1.ConditionTrue, gatev1alpha1.ReasonAvailable)
		require.Eventually(t, func() bool { return slices.Contains(names(t, session), "symbol_search") }, eventually, tick)
	})

	t.Run("a grant for a capability that does not exist", func(t *testing.T) {
		broken := &gatev1alpha1.MCPGrant{
			ObjectMeta: metav1.ObjectMeta{Name: "broken"},
			Spec:       gatev1alpha1.MCPGrantSpec{Subject: "coding-agent", Capabilities: []string{"typo"}},
		}
		create(t, broken)
		require.Eventually(t, func() bool {
			if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: "broken"}, broken); err != nil {
				return false
			}
			valid := meta.FindStatusCondition(broken.Status.Conditions, gatev1alpha1.ConditionValid)
			return valid != nil && valid.Status == metav1.ConditionFalse && valid.Reason == gatev1alpha1.ReasonCapabilityNotFound
		}, eventually, tick)
		assert.Len(t, names(t, session), 5, "a broken grant must not take away what the working one gives")
	})

	t.Run("revoking the grant", func(t *testing.T) {
		for len(changed) > 0 {
			<-changed
		}
		require.NoError(t, k8sClient.Delete(context.Background(), grant))

		require.Eventually(t, func() bool {
			_, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "source_search"})
			return err != nil
		}, eventually, tick)
		select {
		case <-changed:
		case <-time.After(eventually):
			t.Fatal("the open session was not told that its tools changed")
		}
		assert.Empty(t, names(t, session))
	})
}

// A server that was never reachable says available: false in so many words.
// Status is patched against the previous status, and false never differs from
// the zero value, so without the schema default the field — and the kubectl
// column built on it — would simply be absent.
func TestUnreachableServerReportsNotAvailable(t *testing.T) {
	startController(t)
	server := &gatev1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{Name: "never-reachable"},
		Spec:       gatev1alpha1.MCPServerSpec{Endpoint: "http://127.0.0.1:1/mcp"},
	}
	create(t, server)
	requireReady(t, server, metav1.ConditionFalse, gatev1alpha1.ReasonDiscoveryFailed)

	var raw unstructured.Unstructured
	raw.SetGroupVersionKind(gatev1alpha1.GroupVersion.WithKind("MCPServer"))
	require.NoError(t, k8sClient.Get(context.Background(), types.NamespacedName{Name: server.Name}, &raw))
	available, found, err := unstructured.NestedBool(raw.Object, "status", "available")
	require.NoError(t, err)
	assert.True(t, found, "status.available must be present")
	assert.False(t, available)
}

// The schema refuses what it can, so the controller never sees it.
func TestSchemaValidation(t *testing.T) {
	ctx := context.Background()

	err := k8sClient.Create(ctx, &gatev1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{Name: "not-a-url"},
		Spec:       gatev1alpha1.MCPServerSpec{Endpoint: "source-intelligence:8080"},
	})
	assert.Error(t, err, "an endpoint must be an http(s) URL")

	err = k8sClient.Create(ctx, &gatev1alpha1.MCPToolSet{
		ObjectMeta: metav1.ObjectMeta{Name: "empty"},
		Spec:       gatev1alpha1.MCPToolSetSpec{ServerRef: "artifact"},
	})
	assert.Error(t, err, "a tool set selects at least one tool")

	err = k8sClient.Create(ctx, &gatev1alpha1.MCPToolSet{
		ObjectMeta: metav1.ObjectMeta{Name: "duplicates"},
		Spec:       gatev1alpha1.MCPToolSetSpec{ServerRef: "artifact", Tools: []string{"a", "a"}},
	})
	assert.Error(t, err, "tools are a set")

	err = k8sClient.Create(ctx, &gatev1alpha1.MCPGrant{
		ObjectMeta: metav1.ObjectMeta{Name: "nobody"},
		Spec:       gatev1alpha1.MCPGrantSpec{Capabilities: []string{"code-research"}},
	})
	assert.Error(t, err, "a grant names a subject")
}
