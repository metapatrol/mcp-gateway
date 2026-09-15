package controller

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	mcpv1alpha1 "forgejo.metapatrol.local/platform/mcp-gateway/api/v1alpha1"
	"forgejo.metapatrol.local/platform/mcp-gateway/internal/credentials"
	"forgejo.metapatrol.local/platform/mcp-gateway/internal/mcptest"
)

func TestMCPServerReconciler_ReportsTheDiscoveredCatalog(t *testing.T) {
	backend := mcptest.NewBackend(t, "artifact", "artifact_search", "artifact_get", "artifact_publish")
	c := newFakeClient(mcpServer("artifact", backend.URL))
	r := NewMCPServerReconciler(c, c.Scheme(), newTestRegistry(t))

	res := reconcileNamed(t, r, "artifact")
	assert.Equal(t, mcpv1alpha1.DefaultDiscoveryInterval, res.RequeueAfter, "a healthy server is rediscovered on its interval")

	server := get(t, c, "artifact", &mcpv1alpha1.MCPServer{})
	assert.True(t, server.Status.Available)
	assert.Equal(t, 3, server.Status.DiscoveredTools)
	assert.Regexp(t, `^sha256:[0-9a-f]{64}$`, server.Status.CatalogRevision)
	assert.NotNil(t, server.Status.LastDiscoveryTime)
	assert.Equal(t, int64(1), server.Status.ObservedGeneration)
	ready := condition(t, server.Status.Conditions, mcpv1alpha1.ConditionReady)
	assert.Equal(t, metav1.ConditionTrue, ready.Status)
	assert.Equal(t, mcpv1alpha1.ReasonDiscovered, ready.Reason)

	catalog, _, ok := r.Registry.Catalog("artifact")
	require.True(t, ok, "the catalog must be kept for the tool set reconciler")
	assert.Equal(t, server.Status.CatalogRevision, catalog.Revision)
}

// An unreachable backend is Ready=False, and the status keeps describing the
// last catalog that was actually seen.
func TestMCPServerReconciler_UnreachableBackend(t *testing.T) {
	backend := mcptest.NewBackend(t, "artifact", "artifact_get")
	c := newFakeClient(mcpServer("artifact", backend.URL))
	r := NewMCPServerReconciler(c, c.Scheme(), newTestRegistry(t))
	reconcileNamed(t, r, "artifact")
	before := get(t, c, "artifact", &mcpv1alpha1.MCPServer{})

	backend.Close()
	res := reconcileNamed(t, r, "artifact")
	assert.Less(t, res.RequeueAfter, mcpv1alpha1.DefaultDiscoveryInterval, "a failing server is retried sooner than its interval")

	server := get(t, c, "artifact", &mcpv1alpha1.MCPServer{})
	assert.False(t, server.Status.Available)
	ready := condition(t, server.Status.Conditions, mcpv1alpha1.ConditionReady)
	assert.Equal(t, metav1.ConditionFalse, ready.Status)
	assert.Equal(t, mcpv1alpha1.ReasonDiscoveryFailed, ready.Reason)
	assert.Equal(t, before.Status.CatalogRevision, server.Status.CatalogRevision)
	assert.Equal(t, 1, server.Status.DiscoveredTools)
}

func TestMCPServerReconciler_NeverReachedBackend(t *testing.T) {
	c := newFakeClient(mcpServer("artifact", "http://127.0.0.1:1/mcp"))
	r := NewMCPServerReconciler(c, c.Scheme(), newTestRegistry(t))
	reconcileNamed(t, r, "artifact")

	server := get(t, c, "artifact", &mcpv1alpha1.MCPServer{})
	assert.False(t, server.Status.Available)
	assert.Empty(t, server.Status.CatalogRevision)
	assert.Equal(t, metav1.ConditionFalse, condition(t, server.Status.Conditions, mcpv1alpha1.ConditionReady).Status)
}

func TestMCPServerReconciler_DeletedServerIsForgotten(t *testing.T) {
	backend := mcptest.NewBackend(t, "artifact", "artifact_get")
	c := newFakeClient(mcpServer("artifact", backend.URL))
	r := NewMCPServerReconciler(c, c.Scheme(), newTestRegistry(t))
	reconcileNamed(t, r, "artifact")

	require.NoError(t, c.Delete(t.Context(), mcpServer("artifact", "")))
	reconcileNamed(t, r, "artifact")

	_, ok := r.Registry.Backend("artifact")
	assert.False(t, ok)
}

func TestMCPServer_IntervalIsClamped(t *testing.T) {
	s := mcpServer("x", "http://x")
	assert.Equal(t, mcpv1alpha1.DefaultDiscoveryInterval, s.Interval())
	s.Spec.DiscoveryInterval = &metav1.Duration{Duration: 1}
	assert.Equal(t, mcpv1alpha1.MinDiscoveryInterval, s.Interval())
}

func withAPIKey(s *mcpv1alpha1.MCPServer, secret, key string) *mcpv1alpha1.MCPServer {
	s.Spec.Auth = &mcpv1alpha1.BackendAuth{
		Type:   mcpv1alpha1.BackendAuthAPIKey,
		APIKey: &mcpv1alpha1.APIKeyAuth{SecretRef: mcpv1alpha1.SecretKeyRef{Name: secret, Key: key}},
	}
	return s
}

func TestMCPServerReconciler_AuthenticatesWithASecret(t *testing.T) {
	backend := mcptest.NewBackend(t, "memory", "memory_search")
	backend.RequireBearer(map[string]string{"k-123": "controller"})
	c := newFakeClient(
		withAPIKey(mcpServer("memory", backend.URL), "memory-credentials", "token"),
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "memory-credentials", Namespace: "mcp-gateway"},
			Data:       map[string][]byte{"token": []byte("k-123")},
		},
	)
	r := NewMCPServerReconciler(c, c.Scheme(), newTestRegistryWithSecrets(t, credentials.NewSecretReader(c, "mcp-gateway")))
	reconcileNamed(t, r, "memory")

	server := get(t, c, "memory", &mcpv1alpha1.MCPServer{})
	assert.True(t, server.Status.Available)
	assert.Equal(t, mcpv1alpha1.ReasonDiscovered, condition(t, server.Status.Conditions, mcpv1alpha1.ConditionReady).Reason)
}

// A Secret that is not there is something to fix, not an outage to wait out,
// and the condition says which Secret.
func TestMCPServerReconciler_MissingCredentials(t *testing.T) {
	backend := mcptest.NewBackend(t, "memory", "memory_search")
	c := newFakeClient(withAPIKey(mcpServer("memory", backend.URL), "memory-credentials", "token"))
	r := NewMCPServerReconciler(c, c.Scheme(), newTestRegistryWithSecrets(t, credentials.NewSecretReader(c, "mcp-gateway")))
	reconcileNamed(t, r, "memory")

	server := get(t, c, "memory", &mcpv1alpha1.MCPServer{})
	assert.False(t, server.Status.Available)
	ready := condition(t, server.Status.Conditions, mcpv1alpha1.ConditionReady)
	assert.Equal(t, mcpv1alpha1.ReasonCredentialsUnavailable, ready.Reason)
	assert.Contains(t, ready.Message, `secret "memory-credentials" not found in namespace "mcp-gateway"`)
}

func TestMCPServerReconciler_RefusedCredentials(t *testing.T) {
	backend := mcptest.NewBackend(t, "memory", "memory_search")
	backend.RequireBearer(map[string]string{"k-123": "controller"})
	c := newFakeClient(mcpServer("memory", backend.URL))
	r := NewMCPServerReconciler(c, c.Scheme(), newTestRegistry(t))
	reconcileNamed(t, r, "memory")

	server := get(t, c, "memory", &mcpv1alpha1.MCPServer{})
	ready := condition(t, server.Status.Conditions, mcpv1alpha1.ConditionReady)
	assert.Equal(t, mcpv1alpha1.ReasonUnauthorized, ready.Reason)
	assert.Contains(t, ready.Message, "set spec.auth")
}
