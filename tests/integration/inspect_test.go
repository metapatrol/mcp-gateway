package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"forgejo.metapatrol.local/platform/mcp-gateway/internal/inspect"
)

// TestInspectTokenReview checks the inspect API's authentication against a
// real API server: tokens the kubelet would project, reviewed as the gateway
// reviews them. The unit tests fake the answers; this is where the shape of a
// real one — username, audiences — is confirmed.
func TestInspectTokenReview(t *testing.T) {
	ctx := context.Background()
	for _, name := range []string{"mcp-gateway-ui", "someone-else"} {
		sa := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"}}
		require.NoError(t, k8sClient.Create(ctx, sa))
		t.Cleanup(func() { _ = k8sClient.Delete(ctx, sa) })
	}
	token := func(account, audience string) string {
		t.Helper()
		tr := &authenticationv1.TokenRequest{Spec: authenticationv1.TokenRequestSpec{Audiences: []string{audience}}}
		sa := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: account, Namespace: "default"}}
		require.NoError(t, k8sClient.SubResource("token").Create(ctx, sa, tr))
		require.NotEmpty(t, tr.Status.Token)
		return tr.Status.Token
	}

	reviewer := &inspect.TokenReviewer{
		Client:          k8sClient,
		Audience:        "mcp-gateway-inspect",
		ServiceAccounts: []string{"default/mcp-gateway-ui"},
	}
	h := reviewer.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	call := func(token string) int {
		req := httptest.NewRequest(http.MethodGet, "/api/v1alpha1/servers", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}

	assert.Equal(t, http.StatusNoContent, call(token("mcp-gateway-ui", "mcp-gateway-inspect")))
	assert.Equal(t, http.StatusUnauthorized, call(token("mcp-gateway-ui", "some-other-api")), "another audience")
	assert.Equal(t, http.StatusUnauthorized, call(token("someone-else", "mcp-gateway-inspect")), "another ServiceAccount")
	assert.Equal(t, http.StatusUnauthorized, call("not-a-token"))
}
