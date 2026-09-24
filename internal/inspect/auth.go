package inspect

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	authenticationv1 "k8s.io/api/authentication/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"forgejo.metapatrol.local/platform/mcp-gateway/internal/inspect/api"
	"forgejo.metapatrol.local/platform/mcp-gateway/internal/metrics"
)

// The inspect API shows every subject's grants and every backend's catalog,
// so only the console's server may read it. It proves who it is with a
// ServiceAccount token issued for this API's audience, and the gateway asks
// the API server whether the token is valid and whose it is: no secret of its
// own to distribute, and a token the kubelet rotates.
//
// A token for another audience is refused, even the pod's default token. That
// token is good for the Kubernetes API, and one that also opened this API
// would let anything that captured it do both.

// TokenReviewer authenticates requests by TokenReview.
type TokenReviewer struct {
	// Client creates TokenReviews.
	Client client.Client
	// Audience the token must have been issued for.
	Audience string
	// ServiceAccounts that may read the API, as namespace/name.
	ServiceAccounts []string
	// TTL is how long an accepted token is remembered, which is also the
	// longest a deleted ServiceAccount keeps access. Defaults to 1m.
	TTL time.Duration
	// Now defaults to time.Now.
	Now func() time.Time

	mu       sync.Mutex
	accepted map[[sha256.Size]byte]time.Time
}

// maxRemembered bounds the accepted-token cache. Tokens rotate, and a replica
// that has been up for weeks must not keep every one it has seen.
const maxRemembered = 256

// Middleware lets through the requests whose bearer token belongs to one of
// the allowed ServiceAccounts, and answers every other with a 401.
func (t *TokenReviewer) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || token == "" {
			metrics.InspectAuthFailures.WithLabelValues("missing_token").Inc()
			unauthorized(w, "a bearer token is required")
			return
		}
		if reason, err := t.check(r.Context(), token); err != nil {
			metrics.InspectAuthFailures.WithLabelValues(reason).Inc()
			unauthorized(w, err.Error())
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (t *TokenReviewer) now() time.Time {
	if t.Now != nil {
		return t.Now()
	}
	return time.Now()
}

// check returns why a token is refused, as a metric label, and the error to
// answer with.
func (t *TokenReviewer) check(ctx context.Context, token string) (string, error) {
	key := sha256.Sum256([]byte(token))
	now := t.now()

	t.mu.Lock()
	until, ok := t.accepted[key]
	t.mu.Unlock()
	if ok && now.Before(until) {
		return "", nil
	}

	review := &authenticationv1.TokenReview{
		Spec: authenticationv1.TokenReviewSpec{Token: token, Audiences: []string{t.Audience}},
	}
	if err := t.Client.Create(ctx, review); err != nil {
		return "review_failed", fmt.Errorf("the token could not be reviewed: %w", err)
	}
	st := review.Status
	if !st.Authenticated {
		return "not_authenticated", fmt.Errorf("the token was not accepted for audience %s", t.Audience)
	}
	if !slices.Contains(st.Audiences, t.Audience) {
		return "wrong_audience", fmt.Errorf("the token was not issued for audience %s", t.Audience)
	}
	account, ok := strings.CutPrefix(st.User.Username, "system:serviceaccount:")
	if !ok {
		return "not_service_account", fmt.Errorf("%s is not a ServiceAccount", st.User.Username)
	}
	account = strings.Replace(account, ":", "/", 1)
	if !slices.Contains(t.ServiceAccounts, account) {
		return "service_account_not_allowed", fmt.Errorf("ServiceAccount %s may not read this API", account)
	}

	ttl := t.TTL
	if ttl <= 0 {
		ttl = time.Minute
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.accepted == nil {
		t.accepted = map[[sha256.Size]byte]time.Time{}
	}
	if len(t.accepted) >= maxRemembered {
		for k, until := range t.accepted {
			if !now.Before(until) {
				delete(t.accepted, k)
			}
		}
		if len(t.accepted) >= maxRemembered {
			clear(t.accepted)
		}
	}
	t.accepted[key] = now.Add(ttl)
	return "", nil
}

func unauthorized(w http.ResponseWriter, detail string) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="mcp-gateway-inspect"`)
	writeProblem(w, http.StatusUnauthorized, detail)
}

func writeProblem(w http.ResponseWriter, status int, detail string) {
	p := api.Problem{Title: http.StatusText(status), Status: status}
	if detail != "" {
		p.Detail = &detail
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(p)
}
