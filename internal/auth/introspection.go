package auth

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
)

// introspector checks opaque tokens against an RFC 7662 endpoint.
//
// Answers are cached by a hash of the token, never the token itself. A cached
// answer lives for the configured TTL or until the token expires, whichever is
// sooner — the TTL is therefore also the longest a revoked token keeps working.
// Refusals are cached too: a client retrying with a dead token should not cost a
// round trip to the provider each time.
type introspector struct {
	cfg Config

	mu    sync.Mutex
	cache map[[sha256.Size]byte]cached
}

type cached struct {
	info    *sdkauth.TokenInfo
	err     error
	expires time.Time
}

// maxCachedTokens bounds the cache. Past it, expired entries are swept, and if
// that does not help the cache starts over — crude, and enough: the bound
// exists to stop a flood of junk tokens from growing memory, not to be an LRU.
const maxCachedTokens = 10_000

func newIntrospector(cfg Config) *introspector {
	return &introspector{cfg: cfg, cache: map[[sha256.Size]byte]cached{}}
}

func (i *introspector) introspect(ctx context.Context, token string) (*sdkauth.TokenInfo, error) {
	key := sha256.Sum256([]byte(token))
	now := time.Now()

	i.mu.Lock()
	if c, ok := i.cache[key]; ok && now.Before(c.expires) {
		i.mu.Unlock()
		return c.info, c.err
	}
	i.mu.Unlock()

	info, expires, err := i.ask(ctx, token)
	var down *unavailable
	if errors.As(err, &down) {
		// The provider's absence is not an answer about the token.
		return nil, err
	}

	ttl := now.Add(i.cfg.IntrospectionCacheTTL)
	if !expires.IsZero() && expires.Before(ttl) {
		ttl = expires
	}
	i.mu.Lock()
	if len(i.cache) >= maxCachedTokens {
		for k, c := range i.cache {
			if !now.Before(c.expires) {
				delete(i.cache, k)
			}
		}
		if len(i.cache) >= maxCachedTokens {
			i.cache = map[[sha256.Size]byte]cached{}
		}
	}
	i.cache[key] = cached{info: info, err: err, expires: ttl}
	i.mu.Unlock()
	return info, err
}

// ask performs one introspection request.
func (i *introspector) ask(ctx context.Context, token string) (*sdkauth.TokenInfo, time.Time, error) {
	form := url.Values{"token": {token}, "token_type_hint": {"access_token"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, i.cfg.IntrospectionURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, time.Time{}, &unavailable{err: err}
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if i.cfg.IntrospectionClientID != "" {
		req.SetBasicAuth(url.QueryEscape(i.cfg.IntrospectionClientID), url.QueryEscape(i.cfg.IntrospectionClientSecret))
	}

	resp, err := i.cfg.HTTPClient.Do(req)
	if err != nil {
		return nil, time.Time{}, &unavailable{err: err}
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, time.Time{}, &unavailable{err: fmt.Errorf("introspection endpoint answered %d", resp.StatusCode)}
	}

	var claims map[string]any
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&claims); err != nil {
		return nil, time.Time{}, &unavailable{err: fmt.Errorf("decoding introspection response: %w", err)}
	}
	return i.evaluate(claims)
}

// evaluate applies the same checks a JWT gets to an introspection response.
// The provider has already vouched for signature and expiry by saying active,
// and exp is checked anyway: it is cheap, and a cached answer outlives the
// moment it was given.
func (i *introspector) evaluate(claims map[string]any) (*sdkauth.TokenInfo, time.Time, error) {
	if active, _ := claims["active"].(bool); !active {
		return nil, time.Time{}, reject(ReasonInactive)
	}
	// A refresh token is active too, and is not a credential for this API.
	if use, ok := claims["token_use"].(string); ok && use != "access_token" {
		return nil, time.Time{}, reject(ReasonInactive)
	}

	var expiration time.Time
	if exp, ok := claims["exp"].(float64); ok {
		expiration = time.Unix(int64(exp), 0)
		if time.Now().After(expiration.Add(i.cfg.ClockSkew)) {
			return nil, time.Time{}, reject(ReasonExpired)
		}
	}
	if nbf, ok := claims["nbf"].(float64); ok && time.Now().Add(i.cfg.ClockSkew).Before(time.Unix(int64(nbf), 0)) {
		return nil, time.Time{}, reject(ReasonNotYetValid)
	}
	if i.cfg.Issuer != "" {
		if iss, _ := claims["iss"].(string); iss != i.cfg.Issuer {
			return nil, time.Time{}, reject(ReasonIssuer)
		}
	}
	if i.cfg.Audience != "" && !audienceMatches(claims["aud"], i.cfg.Audience) {
		return nil, time.Time{}, reject(ReasonAudience)
	}

	// exp is optional in RFC 7662. Without it the provider's "active" is the
	// whole answer, and the cache TTL bounds how long it is trusted.
	info, err := tokenInfo(i.cfg, claims, expiration)
	return info, expiration, err
}
