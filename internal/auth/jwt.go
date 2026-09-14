package auth

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/lestrrat-go/jwx/v3/jws"
	"github.com/lestrrat-go/jwx/v3/jwt"
	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
)

// keySet is the provider's JWKS, fetched on demand and kept.
//
// It is refreshed on a schedule and additionally whenever a token fails to
// verify, because that is what a rotated signing key looks like from here: a
// valid token under a key ID this copy has never seen. The second kind of
// refresh is rate-limited, or anyone holding a forged token could make the
// gateway hammer the provider by sending it.
type keySet struct {
	url    string
	client *http.Client

	// refreshInterval is how old a key set may get before it is re-fetched
	// ahead of use. minRefreshInterval spaces out refreshes forced by a
	// verification failure.
	refreshInterval    time.Duration
	minRefreshInterval time.Duration

	mu        sync.Mutex
	set       jwk.Set
	fetchedAt time.Time
}

func newKeySet(url string, client *http.Client) *keySet {
	return &keySet{
		url:                url,
		client:             client,
		refreshInterval:    15 * time.Minute,
		minRefreshInterval: 30 * time.Second,
	}
}

// get returns the key set, fetching it when there is none or it is due.
//
// A failed scheduled refresh keeps the set it has. The provider being briefly
// unreachable should not turn every valid token away while the keys that sign
// them are still in hand.
func (k *keySet) get(ctx context.Context) (jwk.Set, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.set != nil && time.Since(k.fetchedAt) < k.refreshInterval {
		return k.set, nil
	}
	set, err := jwk.Fetch(ctx, k.url, jwk.WithHTTPClient(k.client))
	if err != nil {
		if k.set != nil {
			return k.set, nil
		}
		return nil, err
	}
	k.set, k.fetchedAt = set, time.Now()
	return set, nil
}

// refresh re-fetches the key set after a verification failure, unless that was
// done too recently. It reports whether a new set was fetched.
func (k *keySet) refresh(ctx context.Context) (jwk.Set, bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if time.Since(k.fetchedAt) < k.minRefreshInterval {
		return k.set, false
	}
	set, err := jwk.Fetch(ctx, k.url, jwk.WithHTTPClient(k.client))
	if err != nil {
		// Count the attempt, so a provider that is down is not asked again
		// on every bad token either.
		k.fetchedAt = time.Now()
		return k.set, false
	}
	k.set, k.fetchedAt = set, time.Now()
	return set, true
}

func (v *Verifier) verifyJWT(ctx context.Context, raw string) (*sdkauth.TokenInfo, error) {
	set, err := v.keys.get(ctx)
	if err != nil {
		return nil, &unavailable{err: err}
	}

	token, err := parseJWT(raw, set)
	if err != nil {
		if refreshed, ok := v.keys.refresh(ctx); ok {
			token, err = parseJWT(raw, refreshed)
		}
	}
	if err != nil {
		if errors.Is(err, jwt.ParseError()) && !errors.Is(err, jws.VerificationError()) {
			return nil, reject(ReasonMalformed)
		}
		return nil, reject(ReasonSignature)
	}

	opts := []jwt.ValidateOption{jwt.WithAcceptableSkew(v.cfg.ClockSkew)}
	if v.cfg.Issuer != "" {
		opts = append(opts, jwt.WithIssuer(v.cfg.Issuer))
	}
	if v.cfg.Audience != "" {
		opts = append(opts, jwt.WithAudience(v.cfg.Audience))
	}
	if err := jwt.Validate(token, opts...); err != nil {
		switch {
		case errors.Is(err, jwt.TokenExpiredError()):
			return nil, reject(ReasonExpired)
		case errors.Is(err, jwt.TokenNotYetValidError()):
			return nil, reject(ReasonNotYetValid)
		case errors.Is(err, jwt.InvalidIssuerError()):
			return nil, reject(ReasonIssuer)
		case errors.Is(err, jwt.InvalidAudienceError()):
			return nil, reject(ReasonAudience)
		default:
			return nil, reject(ReasonMalformed)
		}
	}

	claims := make(map[string]any, len(token.Keys()))
	for _, key := range token.Keys() {
		var value any
		if err := token.Get(key, &value); err == nil {
			claims[key] = value
		}
	}
	// Registered claims come back typed; the subject and scope readers want
	// the JSON shapes.
	if sub, ok := token.Subject(); ok {
		claims["sub"] = sub
	}
	// RFC 9068 requires exp on a JWT access token. One without it would be
	// valid forever, which no client credentials token has reason to be.
	expiration, ok := token.Expiration()
	if !ok {
		return nil, reject(ReasonNoExpiration)
	}
	return tokenInfo(v.cfg, claims, expiration)
}

// parseJWT verifies the signature against the key set.
//
// The algorithm is taken from the key, never from the token. A token cannot
// pick "none", nor ask for an HMAC check keyed with the provider's public key:
// an RSA key only verifies RSA signatures.
func parseJWT(raw string, set jwk.Set) (jwt.Token, error) {
	return jwt.ParseString(raw,
		jwt.WithKeySet(set, jws.WithInferAlgorithmFromKey(true)),
		jwt.WithValidate(false),
	)
}
