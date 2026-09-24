package inspect

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	authenticationv1 "k8s.io/api/authentication/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"forgejo.metapatrol.local/platform/mcp-gateway/internal/metrics"
)

// reviews answers TokenReviews from a table of token → (username, audiences),
// as the API server would, and counts them.
func reviews(t *testing.T, tokens map[string]authenticationv1.TokenReviewStatus, count *int) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, authenticationv1.AddToScheme(scheme))
	return interceptor.NewClient(fake.NewClientBuilder().WithScheme(scheme).Build(), interceptor.Funcs{
		Create: func(_ context.Context, _ client.WithWatch, obj client.Object, _ ...client.CreateOption) error {
			*count++
			review := obj.(*authenticationv1.TokenReview)
			review.Status = tokens[review.Spec.Token]
			return nil
		},
	})
}

func accepted(username, audience string) authenticationv1.TokenReviewStatus {
	return authenticationv1.TokenReviewStatus{
		Authenticated: true,
		User:          authenticationv1.UserInfo{Username: username},
		Audiences:     []string{audience},
	}
}

func TestTokenReviewer(t *testing.T) {
	var count int
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	tr := &TokenReviewer{
		Client: reviews(t, map[string]authenticationv1.TokenReviewStatus{
			"ui":        accepted("system:serviceaccount:agent-system:mcp-gateway-ui", "mcp-gateway-inspect"),
			"other-sa":  accepted("system:serviceaccount:agent-system:someone-else", "mcp-gateway-inspect"),
			"a-person":  accepted("alice", "mcp-gateway-inspect"),
			"wrong-aud": accepted("system:serviceaccount:agent-system:mcp-gateway-ui", "https://kubernetes.default.svc"),
		}, &count),
		Audience:        "mcp-gateway-inspect",
		ServiceAccounts: []string{"agent-system/mcp-gateway-ui"},
		TTL:             time.Minute,
		Now:             func() time.Time { return now },
	}
	h := tr.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	call := func(header string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/v1alpha1/servers", nil)
		if header != "" {
			req.Header.Set("Authorization", header)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	assert.Equal(t, http.StatusNoContent, call("Bearer ui").Code)
	refusals := func(reason string) float64 {
		return testutil.ToFloat64(metrics.InspectAuthFailures.WithLabelValues(reason))
	}
	before := map[string]float64{}
	reasons := []string{"missing_token", "not_authenticated", "service_account_not_allowed", "not_service_account", "wrong_audience"}
	for _, r := range reasons {
		before[r] = refusals(r)
	}
	for _, header := range []string{"", "Basic dWk6", "Bearer ", "Bearer unknown", "Bearer other-sa", "Bearer a-person", "Bearer wrong-aud"} {
		rec := call(header)
		assert.Equal(t, http.StatusUnauthorized, rec.Code, header)
		assert.Equal(t, "application/problem+json", rec.Header().Get("Content-Type"))
		assert.NotEmpty(t, rec.Header().Get("WWW-Authenticate"))
	}
	// "", Basic and an empty bearer carry no token; each other names its reason.
	want := map[string]float64{"missing_token": 3, "not_authenticated": 1, "service_account_not_allowed": 1, "not_service_account": 1, "wrong_audience": 1}
	for _, r := range reasons {
		assert.Equal(t, before[r]+want[r], refusals(r), r)
	}

	t.Run("an accepted token is remembered for the TTL", func(t *testing.T) {
		count = 0
		call("Bearer ui")
		call("Bearer ui")
		assert.Equal(t, 0, count, "still remembered from the first request")
		now = now.Add(2 * time.Minute)
		call("Bearer ui")
		assert.Equal(t, 1, count, "reviewed again once the TTL passed")
	})

	t.Run("a refused token is not remembered", func(t *testing.T) {
		count = 0
		call("Bearer other-sa")
		call("Bearer other-sa")
		assert.Equal(t, 2, count)
	})
}
