package integration

import (
	"context"
	"net/http"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gatev1alpha1 "forgejo.metapatrol.local/platform/mcp-gate/api/v1alpha1"
	"forgejo.metapatrol.local/platform/mcp-gate/internal/mcptest"
)

// A backend that wants credentials, end to end: the controller reports the
// missing Secret, picks it up once it exists, and the gateway passes each
// agent's own token to the backend's tool calls.
func TestBackendAuth(t *testing.T) {
	memory := mcptest.NewBackend(t, "memory", "memory_search")
	memory.RequireBearer(map[string]string{
		"discovery-key": "mcp-gate",
		"coding-agent":  "coding-agent",
	})
	var mu sync.Mutex
	var callers []string
	memory.Guard(func(r *http.Request) int {
		if mcptest.RPCMethod(r) == "tools/call" {
			mu.Lock()
			callers = append(callers, r.Header.Get("Authorization"))
			mu.Unlock()
		}
		return 0
	})

	startController(t)
	url, _ := startGateway(t)

	server := &gatev1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{Name: "memory"},
		Spec: gatev1alpha1.MCPServerSpec{
			Endpoint: memory.URL,
			Auth: &gatev1alpha1.BackendAuth{
				Type: gatev1alpha1.BackendAuthPassthrough,
				Passthrough: &gatev1alpha1.PassthroughAuth{Discovery: &gatev1alpha1.DiscoveryAuth{
					APIKey: &gatev1alpha1.APIKeyAuth{SecretRef: gatev1alpha1.SecretKeyRef{Name: "memory-discovery", Key: "MEMORY_KEY"}},
				}},
			},
		},
	}
	create(t, server)

	t.Run("a missing Secret is reported", func(t *testing.T) {
		requireReady(t, server, metav1.ConditionFalse, gatev1alpha1.ReasonCredentialsUnavailable)
	})

	t.Run("the Secret is picked up once it exists", func(t *testing.T) {
		create(t, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "memory-discovery", Namespace: secretNamespace},
			StringData: map[string]string{"MEMORY_KEY": "discovery-key"},
		})
		requireReady(t, server, metav1.ConditionTrue, gatev1alpha1.ReasonDiscovered)
	})

	for _, obj := range []client.Object{
		&gatev1alpha1.MCPToolSet{
			ObjectMeta: metav1.ObjectMeta{Name: "memory"},
			Spec:       gatev1alpha1.MCPToolSetSpec{ServerRef: "memory", Tools: []string{"memory_*"}},
		},
		&gatev1alpha1.MCPCapability{
			ObjectMeta: metav1.ObjectMeta{Name: "memory"},
			Spec:       gatev1alpha1.MCPCapabilitySpec{ToolSets: []string{"memory"}},
		},
		&gatev1alpha1.MCPGrant{
			ObjectMeta: metav1.ObjectMeta{Name: "memory"},
			Spec:       gatev1alpha1.MCPGrantSpec{Subject: "coding-agent", Capabilities: []string{"memory"}},
		},
	} {
		create(t, obj)
	}

	t.Run("tool calls carry the agent's own token", func(t *testing.T) {
		session := connect(t, url, "coding-agent", nil)
		require.Eventually(t, func() bool { return len(names(t, session)) == 1 }, eventually, tick)

		res, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "memory_search", Arguments: map[string]any{}})
		require.NoError(t, err)
		assert.Equal(t, "memory/memory_search {} as coding-agent", res.Content[0].(*mcp.TextContent).Text)
		mu.Lock()
		defer mu.Unlock()
		assert.Equal(t, []string{"Bearer coding-agent"}, callers)
	})
}

// The schema refuses an auth block that does not hold together.
func TestBackendAuthSchemaValidation(t *testing.T) {
	ctx := context.Background()
	key := gatev1alpha1.SecretKeyRef{Name: "memory", Key: "token"}
	cc := func() *gatev1alpha1.ClientCredentialsAuth {
		return &gatev1alpha1.ClientCredentialsAuth{TokenURL: "https://id.example.com/oauth2/token", ClientID: "gateway", ClientSecretRef: key}
	}
	server := func(name string, auth *gatev1alpha1.BackendAuth) *gatev1alpha1.MCPServer {
		return &gatev1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Spec:       gatev1alpha1.MCPServerSpec{Endpoint: "http://memory.svc/mcp", Auth: auth},
		}
	}

	refused := map[string]*gatev1alpha1.BackendAuth{
		"client-credentials-without-its-block": {Type: gatev1alpha1.BackendAuthClientCredentials},
		"api-key-without-its-block":            {Type: gatev1alpha1.BackendAuthAPIKey},
		"a-block-of-another-type":              {Type: gatev1alpha1.BackendAuthNone, APIKey: &gatev1alpha1.APIKeyAuth{SecretRef: key}},
		"passthrough-block-with-api-key-type": {
			Type:        gatev1alpha1.BackendAuthAPIKey,
			APIKey:      &gatev1alpha1.APIKeyAuth{SecretRef: key},
			Passthrough: &gatev1alpha1.PassthroughAuth{},
		},
		"client-id-twice": {Type: gatev1alpha1.BackendAuthClientCredentials, ClientCredentials: func() *gatev1alpha1.ClientCredentialsAuth {
			c := cc()
			c.ClientIDRef = &key
			return c
		}()},
		"no-client-id": {Type: gatev1alpha1.BackendAuthClientCredentials, ClientCredentials: func() *gatev1alpha1.ClientCredentialsAuth {
			c := cc()
			c.ClientID = ""
			return c
		}()},
		"two-discovery-credentials": {Type: gatev1alpha1.BackendAuthPassthrough, Passthrough: &gatev1alpha1.PassthroughAuth{
			Discovery: &gatev1alpha1.DiscoveryAuth{ClientCredentials: cc(), APIKey: &gatev1alpha1.APIKeyAuth{SecretRef: key}},
		}},
		"no-discovery-credential": {Type: gatev1alpha1.BackendAuthPassthrough, Passthrough: &gatev1alpha1.PassthroughAuth{
			Discovery: &gatev1alpha1.DiscoveryAuth{},
		}},
		"a-secret-ref-without-a-key": {Type: gatev1alpha1.BackendAuthAPIKey, APIKey: &gatev1alpha1.APIKeyAuth{
			SecretRef: gatev1alpha1.SecretKeyRef{Name: "memory"},
		}},
		"an-unknown-type": {Type: "Kerberos"},
	}
	for name, auth := range refused {
		assert.Error(t, k8sClient.Create(ctx, server(name, auth)), name)
	}

	accepted := map[string]*gatev1alpha1.BackendAuth{
		"passthrough":        {Type: gatev1alpha1.BackendAuthPassthrough},
		"client-credentials": {Type: gatev1alpha1.BackendAuthClientCredentials, ClientCredentials: cc()},
		"api-key-bare":       {Type: gatev1alpha1.BackendAuthAPIKey, APIKey: &gatev1alpha1.APIKeyAuth{SecretRef: key, Prefix: ptr.To("")}},
		"passthrough-with-discovery": {Type: gatev1alpha1.BackendAuthPassthrough, Passthrough: &gatev1alpha1.PassthroughAuth{
			Discovery: &gatev1alpha1.DiscoveryAuth{ClientCredentials: cc()},
		}},
	}
	for name, auth := range accepted {
		create(t, server(name, auth))
	}

	var got gatev1alpha1.MCPServer
	require.NoError(t, k8sClient.Get(ctx, types.NamespacedName{Name: "api-key-bare"}, &got))
	assert.Equal(t, "Authorization", got.Spec.Auth.APIKey.Header, "the header defaults")
	require.NotNil(t, got.Spec.Auth.APIKey.Prefix, "an empty prefix is kept, not dropped")
	assert.Empty(t, *got.Spec.Auth.APIKey.Prefix)

	// Written as a manifest would be: the typed client cannot leave type out.
	empty := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": gatev1alpha1.GroupVersion.String(),
		"kind":       "MCPServer",
		"metadata":   map[string]any{"name": "empty-auth"},
		"spec":       map[string]any{"endpoint": "http://memory.svc/mcp", "auth": map[string]any{}},
	}}
	create(t, empty)
	require.NoError(t, k8sClient.Get(ctx, types.NamespacedName{Name: "empty-auth"}, &got))
	assert.Equal(t, gatev1alpha1.BackendAuthNone, got.Spec.Auth.Type, "the type defaults to None")
}
