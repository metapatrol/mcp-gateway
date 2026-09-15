package auth

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/lestrrat-go/jwx/v4/jwk"
	"github.com/lestrrat-go/jwx/v4/jws"
	"github.com/lestrrat-go/jwx/v4/jwt"
	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
)

// keySet is the provider's JWKS, fetched on demand and kept.
//
// It is refreshed on a schedule and additionally whenever a token fails to
// verify, because that is what a rotated signing key looks like from here: a
// valid token under a key ID this copy has never seen. The second kind of
// refresh is rate-limited, or anyone holding a forged token could make the
// gateway hammer the provider by sending it.
//
// Every fetch that brings a set different from the one held starts a new
// generation. Verified tokens are remembered per generation, so a changed key
// set — a key withdrawn, above all — invalidates them all at once.
type keySet struct {
	url    string
	client *http.Client

	// refreshInterval is how old a key set may get before it is re-fetched
	// ahead of use. minRefreshInterval spaces out refreshes forced by a
	// verification failure.
	refreshInterval    time.Duration
	minRefreshInterval time.Duration

	mu         sync.Mutex
	set        jwk.Set
	fetchedAt  time.Time
	digest     [sha256.Size]byte
	generation uint64
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
func (k *keySet) get(ctx context.Context) (jwk.Set, uint64, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.set != nil && time.Since(k.fetchedAt) < k.refreshInterval {
		return k.set, k.generation, nil
	}
	set, err := k.fetch(ctx)
	if err != nil {
		if k.set != nil {
			return k.set, k.generation, nil
		}
		return nil, 0, err
	}
	k.store(set)
	return set, k.generation, nil
}

// refresh re-fetches the key set after a verification failure, unless that was
// done too recently. It reports whether a new set was fetched.
func (k *keySet) refresh(ctx context.Context) (jwk.Set, uint64, bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if time.Since(k.fetchedAt) < k.minRefreshInterval {
		return k.set, k.generation, false
	}
	set, err := k.fetch(ctx)
	if err != nil {
		// Count the attempt, so a provider that is down is not asked again
		// on every bad token either.
		k.fetchedAt = time.Now()
		return k.set, k.generation, false
	}
	k.store(set)
	return set, k.generation, true
}

// maxKeySetBytes bounds the JWKS document read from the provider.
const maxKeySetBytes = 1 << 20

// fetch reads the key set from the provider.
func (k *keySet) fetch(ctx context.Context) (jwk.Set, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, k.url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := k.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("JWKS endpoint answered %d", resp.StatusCode)
	}
	return jwk.ParseReader(io.LimitReader(resp.Body, maxKeySetBytes))
}

// store keeps a freshly fetched set, starting a new generation if it differs
// from the one held. A set that does not encode is taken as different: that
// errs towards verifying tokens again, never towards trusting them longer.
func (k *keySet) store(set jwk.Set) {
	k.fetchedAt = time.Now()
	k.set = set
	encoded, err := json.Marshal(set)
	digest := sha256.Sum256(encoded)
	if err != nil || digest != k.digest || k.generation == 0 {
		k.digest = digest
		k.generation++
	}
}

// verifyJWT verifies a JWT, or recalls having verified it.
//
// An agent presents the same token on every request for the token's whole
// lifetime, and checking its signature again each time is the largest part of
// the work the gateway does per call. So a token that verified is remembered
// until it expires, for as long as the key set it verified against is the one
// held. Tokens that fail are checked in full every time: a flood of forged
// tokens must not fill the cache.
func (v *Verifier) verifyJWT(ctx context.Context, raw string) (*sdkauth.TokenInfo, error) {
	set, generation, err := v.keys.get(ctx)
	if err != nil {
		return nil, &unavailable{err: err}
	}
	key := tokenKey(raw)
	now := time.Now()
	if known, ok := v.verified.get(key, generation, now); ok {
		return known.info, nil
	}

	info, expiration, generation, err := v.checkJWT(ctx, raw, set, generation)
	if err != nil {
		return nil, err
	}
	v.verified.put(key, verdict{info: info, expires: expiration, generation: generation}, now)
	return info, nil
}

// checkJWT verifies a JWT in full: signature, registered claims, subject. It
// returns the generation of the key set that verified it, which is newer than
// the one passed in when a failure made it refresh the set.
func (v *Verifier) checkJWT(ctx context.Context, raw string, set jwk.Set, generation uint64) (*sdkauth.TokenInfo, time.Time, uint64, error) {
	token, err := parseJWT(raw, set)
	if err != nil {
		if refreshed, refreshedGeneration, ok := v.keys.refresh(ctx); ok {
			token, err = parseJWT(raw, refreshed)
			generation = refreshedGeneration
		}
	}
	if err != nil {
		// Malformed only if the token does not even decode. jwx reports a key
		// ID the set does not contain as a parse error, and a token that
		// decodes but names a key nobody published is a failed signature,
		// not a broken token — the difference between a client bug and a
		// forgery in mcp_gate_auth_failures_total.
		if _, decodeErr := jwt.ParseInsecure([]byte(raw)); decodeErr != nil {
			return nil, time.Time{}, 0, reject(ReasonMalformed)
		}
		return nil, time.Time{}, 0, reject(ReasonSignature)
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
		case errors.Is(err, jwt.TokenExpiredError{}):
			return nil, time.Time{}, 0, reject(ReasonExpired)
		case errors.Is(err, jwt.TokenNotYetValidError{}):
			return nil, time.Time{}, 0, reject(ReasonNotYetValid)
		case errors.Is(err, jwt.InvalidIssuerError{}):
			return nil, time.Time{}, 0, reject(ReasonIssuer)
		case errors.Is(err, jwt.InvalidAudienceError{}):
			return nil, time.Time{}, 0, reject(ReasonAudience)
		default:
			return nil, time.Time{}, 0, reject(ReasonMalformed)
		}
	}

	claims := map[string]any{}
	for name, value := range token.Claims() {
		claims[name] = value
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
		return nil, time.Time{}, 0, reject(ReasonNoExpiration)
	}
	info, err := tokenInfo(v.cfg, claims, expiration)
	if err != nil {
		return nil, time.Time{}, 0, err
	}
	return info, expiration, generation, nil
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
