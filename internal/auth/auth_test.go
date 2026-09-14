package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/lestrrat-go/jwx/v3/jwt"
	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testIssuer   = "https://oauth.example.com/"
	testAudience = "https://mcp-gate.example.com/mcp"
)

// issuer is an OAuth provider as far as the gateway can tell: a JWKS endpoint,
// and a key to sign tokens with.
type issuer struct {
	t       *testing.T
	key     jwk.Key
	jwks    *httptest.Server
	fetches atomic.Int64
	// published is the key set the JWKS endpoint serves.
	published atomic.Pointer[jwk.Set]
}

func newIssuer(t *testing.T) *issuer {
	t.Helper()
	i := &issuer{t: t}
	i.rotate("key-1")
	i.jwks = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		i.fetches.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(*i.published.Load())
	}))
	t.Cleanup(i.jwks.Close)
	return i
}

// rotate replaces the signing key and publishes only the new one.
func (i *issuer) rotate(kid string) {
	raw, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(i.t, err)
	key, err := jwk.Import(raw)
	require.NoError(i.t, err)
	require.NoError(i.t, key.Set(jwk.KeyIDKey, kid))
	require.NoError(i.t, key.Set(jwk.AlgorithmKey, jwa.RS256()))
	i.key = key

	public, err := key.PublicKey()
	require.NoError(i.t, err)
	set := jwk.NewSet()
	require.NoError(i.t, set.AddKey(public))
	i.published.Store(&set)
}

// token signs a token with the standard claims set to sensible values, then
// applied edits.
func (i *issuer) token(edit func(jwt.Token)) string {
	i.t.Helper()
	tok := jwt.New()
	now := time.Now()
	require.NoError(i.t, tok.Set(jwt.IssuerKey, testIssuer))
	require.NoError(i.t, tok.Set(jwt.AudienceKey, []string{testAudience}))
	require.NoError(i.t, tok.Set(jwt.SubjectKey, "coding-agent"))
	require.NoError(i.t, tok.Set(jwt.IssuedAtKey, now))
	require.NoError(i.t, tok.Set(jwt.ExpirationKey, now.Add(time.Hour)))
	require.NoError(i.t, tok.Set("scope", "mcp:tools other"))
	if edit != nil {
		edit(tok)
	}
	signed, err := jwt.Sign(tok, jwt.WithKey(jwa.RS256(), i.key))
	require.NoError(i.t, err)
	return string(signed)
}

func (i *issuer) verifier(t *testing.T, edit func(*Config)) *Verifier {
	t.Helper()
	cfg := Config{Issuer: testIssuer, Audience: testAudience, JWKSURL: i.jwks.URL}
	if edit != nil {
		edit(&cfg)
	}
	v, err := NewVerifier(cfg)
	require.NoError(t, err)
	return v
}

func assertRejected(t *testing.T, err error, reason string) {
	t.Helper()
	require.Error(t, err)
	assert.ErrorIs(t, err, sdkauth.ErrInvalidToken, "a refused token must make the middleware answer 401")
	var r *rejection
	require.ErrorAs(t, err, &r)
	assert.Equal(t, reason, r.reason)
}

func TestJWT_ValidTokenYieldsTheSubject(t *testing.T) {
	iss := newIssuer(t)
	v := iss.verifier(t, nil)

	info, err := v.Verify(context.Background(), iss.token(nil), nil)
	require.NoError(t, err)
	assert.Equal(t, "coding-agent", info.UserID)
	assert.Equal(t, []string{"mcp:tools", "other"}, info.Scopes)
	assert.WithinDuration(t, time.Now().Add(time.Hour), info.Expiration, 5*time.Second)
}

func TestJWT_Rejections(t *testing.T) {
	iss := newIssuer(t)
	v := iss.verifier(t, nil)
	other := newIssuer(t)
	stranger := newIssuer(t)
	stranger.rotate("a-key-id-nobody-published")

	cases := []struct {
		name   string
		token  string
		reason string
	}{
		{"expired", iss.token(func(tok jwt.Token) { _ = tok.Set(jwt.ExpirationKey, time.Now().Add(-time.Hour)) }), ReasonExpired},
		{"not yet valid", iss.token(func(tok jwt.Token) { _ = tok.Set(jwt.NotBeforeKey, time.Now().Add(time.Hour)) }), ReasonNotYetValid},
		{"wrong issuer", iss.token(func(tok jwt.Token) { _ = tok.Set(jwt.IssuerKey, "https://evil.example.com/") }), ReasonIssuer},
		{"token for another service", iss.token(func(tok jwt.Token) { _ = tok.Set(jwt.AudienceKey, "https://other.example.com") }), ReasonAudience},
		{"no subject", iss.token(func(tok jwt.Token) { _ = tok.Remove(jwt.SubjectKey) }), ReasonNoSubject},
		{"no expiration", iss.token(func(tok jwt.Token) { _ = tok.Remove(jwt.ExpirationKey) }), ReasonNoExpiration},
		{"signed by a key the provider never published", other.token(nil), ReasonSignature},
		// jwx reports an unknown key ID as a parse error; it is still a
		// signature that does not verify, not a broken token.
		{"signed under a key ID the provider never published", stranger.token(nil), ReasonSignature},
		{"three segments that do not decode", "not.a.jwt", ReasonMalformed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := v.Verify(context.Background(), tc.token, nil)
			assertRejected(t, err, tc.reason)
		})
	}
}

// The classic: an unsigned token with alg "none". The key decides the
// algorithm, so it has nothing to verify against.
func TestJWT_UnsignedTokenIsRefused(t *testing.T) {
	iss := newIssuer(t)
	v := iss.verifier(t, nil)

	tok := jwt.New()
	_ = tok.Set(jwt.SubjectKey, "coding-agent")
	_ = tok.Set(jwt.IssuerKey, testIssuer)
	_ = tok.Set(jwt.AudienceKey, testAudience)
	_ = tok.Set(jwt.ExpirationKey, time.Now().Add(time.Hour))
	payload, err := json.Marshal(tok)
	require.NoError(t, err)
	unsigned := "eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0." + base64URL(payload) + ".x"

	_, err = v.Verify(context.Background(), unsigned, nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, sdkauth.ErrInvalidToken)
}

// A provider that rotates its signing key issues tokens under a key ID the
// gateway has never seen. The gateway must pick up the new key rather than turn
// valid tokens away until its next scheduled refresh.
func TestJWT_KeyRotationIsPickedUp(t *testing.T) {
	iss := newIssuer(t)
	v := iss.verifier(t, nil)
	v.keys.minRefreshInterval = 0

	_, err := v.Verify(context.Background(), iss.token(nil), nil)
	require.NoError(t, err)

	iss.rotate("key-2")
	info, err := v.Verify(context.Background(), iss.token(nil), nil)
	require.NoError(t, err)
	assert.Equal(t, "coding-agent", info.UserID)
}

// Bad tokens must not turn the gateway into a JWKS load generator.
func TestJWT_ForcedRefreshesAreRateLimited(t *testing.T) {
	iss := newIssuer(t)
	v := iss.verifier(t, nil)
	other := newIssuer(t)

	_, err := v.Verify(context.Background(), iss.token(nil), nil)
	require.NoError(t, err)
	before := iss.fetches.Load()

	for range 20 {
		_, err := v.Verify(context.Background(), other.token(nil), nil)
		require.Error(t, err)
	}
	assert.Equal(t, before, iss.fetches.Load())
}

func TestJWT_ProviderDownIsNotAnInvalidToken(t *testing.T) {
	iss := newIssuer(t)
	v := iss.verifier(t, nil)
	iss.jwks.Close()

	_, err := v.Verify(context.Background(), iss.token(nil), nil)
	require.Error(t, err)
	assert.NotErrorIs(t, err, sdkauth.ErrInvalidToken,
		"a 401 would send the client to fetch a new token from the provider that is down")
}

func TestJWT_SubjectFromANestedClaim(t *testing.T) {
	iss := newIssuer(t)
	v := iss.verifier(t, func(c *Config) { c.SubjectClaim = "ext.subject" })

	info, err := v.Verify(context.Background(), iss.token(func(tok jwt.Token) {
		_ = tok.Set(jwt.SubjectKey, "4f1c9a1e-generated-client-id")
		_ = tok.Set("ext", map[string]any{"subject": "coding-agent"})
	}), nil)
	require.NoError(t, err)
	assert.Equal(t, "coding-agent", info.UserID)
}

func TestJWT_ClaimNamedByURLIsReadWhole(t *testing.T) {
	iss := newIssuer(t)
	v := iss.verifier(t, func(c *Config) { c.SubjectClaim = "https://mcp-gate.example.com/subject" })

	info, err := v.Verify(context.Background(), iss.token(func(tok jwt.Token) {
		_ = tok.Set("https://mcp-gate.example.com/subject", "coding-agent")
	}), nil)
	require.NoError(t, err)
	assert.Equal(t, "coding-agent", info.UserID)
}

func TestOpaqueTokenWithoutIntrospectionIsUnsupported(t *testing.T) {
	iss := newIssuer(t)
	v := iss.verifier(t, nil)

	_, err := v.Verify(context.Background(), "ory_at_opaque", nil)
	assertRejected(t, err, ReasonUnsupported)
}

// introspectionEndpoint answers RFC 7662 requests from a table of tokens.
func introspectionEndpoint(t *testing.T, tokens map[string]map[string]any, calls *atomic.Int64, wantBasic [2]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if wantBasic[0] != "" {
			user, pass, ok := r.BasicAuth()
			if !ok || user != wantBasic[0] || pass != wantBasic[1] {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
		}
		require.NoError(t, r.ParseForm())
		answer, ok := tokens[r.PostForm.Get("token")]
		if !ok {
			answer = map[string]any{"active": false}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(answer)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestIntrospection(t *testing.T) {
	exp := float64(time.Now().Add(time.Hour).Unix())
	tokens := map[string]map[string]any{
		"ory_at_good":     {"active": true, "sub": "coding-agent", "client_id": "coding-agent", "scope": "mcp", "exp": exp, "iss": testIssuer, "aud": []any{testAudience}},
		"ory_at_refresh":  {"active": true, "sub": "coding-agent", "token_use": "refresh_token", "exp": exp, "iss": testIssuer, "aud": []any{testAudience}},
		"ory_at_foreign":  {"active": true, "sub": "coding-agent", "exp": exp, "iss": testIssuer, "aud": []any{"https://other.example.com"}},
		"ory_at_no_sub":   {"active": true, "exp": exp, "iss": testIssuer, "aud": testAudience},
		"ory_at_no_exp":   {"active": true, "sub": "coding-agent", "iss": testIssuer, "aud": testAudience},
		"ory_at_past_exp": {"active": true, "sub": "coding-agent", "exp": float64(time.Now().Add(-time.Hour).Unix()), "iss": testIssuer, "aud": testAudience},
	}
	var calls atomic.Int64
	endpoint := introspectionEndpoint(t, tokens, &calls, [2]string{"mcp-gate", "s3cret"})

	v, err := NewVerifier(Config{
		Issuer:                    testIssuer,
		Audience:                  testAudience,
		IntrospectionURL:          endpoint.URL,
		IntrospectionClientID:     "mcp-gate",
		IntrospectionClientSecret: "s3cret",
	})
	require.NoError(t, err)

	info, err := v.Verify(context.Background(), "ory_at_good", nil)
	require.NoError(t, err)
	assert.Equal(t, "coding-agent", info.UserID)
	assert.Equal(t, "coding-agent", info.Extra["client_id"])

	info, err = v.Verify(context.Background(), "ory_at_no_exp", nil)
	require.NoError(t, err, "exp is optional in an introspection response")
	assert.True(t, info.Expiration.IsZero())

	for token, reason := range map[string]string{
		"ory_at_unknown":  ReasonInactive,
		"ory_at_refresh":  ReasonInactive,
		"ory_at_foreign":  ReasonAudience,
		"ory_at_no_sub":   ReasonNoSubject,
		"ory_at_past_exp": ReasonExpired,
	} {
		_, err := v.Verify(context.Background(), token, nil)
		assertRejected(t, err, reason)
	}
}

func TestIntrospection_AnswersAreCached(t *testing.T) {
	tokens := map[string]map[string]any{
		"ory_at_good": {"active": true, "sub": "coding-agent", "exp": float64(time.Now().Add(time.Hour).Unix())},
	}
	var calls atomic.Int64
	endpoint := introspectionEndpoint(t, tokens, &calls, [2]string{})
	v, err := NewVerifier(Config{IntrospectionURL: endpoint.URL, IntrospectionCacheTTL: time.Minute})
	require.NoError(t, err)

	for range 5 {
		_, err := v.Verify(context.Background(), "ory_at_good", nil)
		require.NoError(t, err)
		_, err = v.Verify(context.Background(), "ory_at_bad", nil)
		require.Error(t, err)
	}
	assert.Equal(t, int64(2), calls.Load(), "one call per distinct token")
}

// The endpoint refusing the gateway's own credentials says nothing about the
// caller's token, and must not be cached as a verdict on it.
func TestIntrospection_EndpointFailureIsNotCached(t *testing.T) {
	tokens := map[string]map[string]any{
		"ory_at_good": {"active": true, "sub": "coding-agent"},
	}
	var calls atomic.Int64
	endpoint := introspectionEndpoint(t, tokens, &calls, [2]string{"mcp-gate", "right"})
	v, err := NewVerifier(Config{IntrospectionURL: endpoint.URL, IntrospectionClientID: "mcp-gate", IntrospectionClientSecret: "wrong"})
	require.NoError(t, err)

	_, err = v.Verify(context.Background(), "ory_at_good", nil)
	require.Error(t, err)
	assert.NotErrorIs(t, err, sdkauth.ErrInvalidToken)

	_, _ = v.Verify(context.Background(), "ory_at_good", nil)
	assert.Equal(t, int64(2), calls.Load())
}

func TestConfigValidate(t *testing.T) {
	assert.Error(t, Config{}.Validate())
	assert.Error(t, Config{IntrospectionURL: "http://x", IntrospectionClientID: "id"}.Validate())
	assert.NoError(t, Config{JWKSURL: "http://x"}.Validate())
}

func base64URL(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}
