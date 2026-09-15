package auth

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/lestrrat-go/jwx/v4/jwt"
	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A token is verified once and recognised afterwards: agents present the same
// token on every request, and its signature is the expensive part.
func TestJWT_AVerifiedTokenIsRemembered(t *testing.T) {
	iss := newIssuer(t)
	v := iss.verifier(t, nil)
	token := iss.token(nil)

	first, err := v.Verify(context.Background(), token, nil)
	require.NoError(t, err)
	again, err := v.Verify(context.Background(), token, nil)
	require.NoError(t, err)
	assert.Same(t, first, again)

	// A full verification allocates some three hundred times; recognising a
	// token must not come close.
	allocs := testing.AllocsPerRun(100, func() {
		_, _ = v.Verify(context.Background(), token, nil)
	})
	assert.Less(t, allocs, 10.0)
}

// Refused tokens are checked in full every time, so a flood of forged or
// misdirected tokens cannot fill the cache.
func TestJWT_ARefusedTokenIsNotRemembered(t *testing.T) {
	iss := newIssuer(t)
	v := iss.verifier(t, nil)
	other := newIssuer(t)

	for _, token := range []string{
		iss.token(func(tok jwt.Token) { _ = tok.Set(jwt.AudienceKey, "https://other.example.com") }),
		other.token(nil),
	} {
		_, err := v.Verify(context.Background(), token, nil)
		require.Error(t, err)
		_, err = v.Verify(context.Background(), token, nil)
		require.Error(t, err)
	}
	assert.Zero(t, v.verified.len())
}

// A remembered token is only remembered until it expires.
func TestJWT_ARememberedTokenExpires(t *testing.T) {
	iss := newIssuer(t)
	v := iss.verifier(t, func(c *Config) { c.ClockSkew = time.Millisecond })
	expires := time.Now().Add(2 * time.Second).Truncate(time.Second)
	token := iss.token(func(tok jwt.Token) { _ = tok.Set(jwt.ExpirationKey, expires) })

	_, err := v.Verify(context.Background(), token, nil)
	require.NoError(t, err)
	time.Sleep(time.Until(expires) + 50*time.Millisecond)

	_, err = v.Verify(context.Background(), token, nil)
	assertRejected(t, err, ReasonExpired)
}

// A key the provider withdraws stops vouching for the tokens it signed as soon
// as the gateway sees the new key set — remembered tokens included.
func TestJWT_AWithdrawnKeyInvalidatesRememberedTokens(t *testing.T) {
	iss := newIssuer(t)
	v := iss.verifier(t, nil)
	v.keys.minRefreshInterval = 0
	underOldKey := iss.token(nil)
	_, err := v.Verify(context.Background(), underOldKey, nil)
	require.NoError(t, err)

	// The provider rotates and publishes only the new key. A token under it
	// makes the gateway fetch the new set.
	iss.rotate("key-2")
	_, err = v.Verify(context.Background(), iss.token(nil), nil)
	require.NoError(t, err)

	_, err = v.Verify(context.Background(), underOldKey, nil)
	assertRejected(t, err, ReasonSignature)
}

// The scheduled refresh counts too, not only the one a failure forces.
func TestJWT_AScheduledRefreshInvalidatesRememberedTokens(t *testing.T) {
	iss := newIssuer(t)
	v := iss.verifier(t, nil)
	underOldKey := iss.token(nil)
	_, err := v.Verify(context.Background(), underOldKey, nil)
	require.NoError(t, err)

	iss.rotate("key-2")
	v.keys.refreshInterval = 0
	_, err = v.Verify(context.Background(), underOldKey, nil)
	assertRejected(t, err, ReasonSignature)
}

// Fetching the same key set again changes nothing: the remembered tokens stay
// remembered.
func TestJWT_AnUnchangedKeySetKeepsRememberedTokens(t *testing.T) {
	iss := newIssuer(t)
	v := iss.verifier(t, nil)
	token := iss.token(nil)
	first, err := v.Verify(context.Background(), token, nil)
	require.NoError(t, err)

	v.keys.refreshInterval = 0
	again, err := v.Verify(context.Background(), token, nil)
	require.NoError(t, err)
	assert.Same(t, first, again)
	assert.Greater(t, iss.fetches.Load(), int64(1), "the key set was fetched again")
}

func TestVerdicts_AreBounded(t *testing.T) {
	c := newVerdicts()
	now := time.Now()
	info := &sdkauth.TokenInfo{UserID: "coding-agent"}

	// Expired entries and entries of an older generation go first.
	for i := range maxCachedTokens / 2 {
		c.put(tokenKey(fmt.Sprintf("expired-%d", i)), verdict{info: info, expires: now.Add(-time.Second), generation: 2}, now)
	}
	for i := range maxCachedTokens / 2 {
		c.put(tokenKey(fmt.Sprintf("old-generation-%d", i)), verdict{info: info, expires: now.Add(time.Hour), generation: 1}, now)
	}
	c.put(tokenKey("current"), verdict{info: info, expires: now.Add(time.Hour), generation: 2}, now)
	assert.Equal(t, 1, c.len())
	_, ok := c.get(tokenKey("current"), 2, now)
	assert.True(t, ok)

	// When everything is still good, the cache starts over rather than grow.
	for i := range maxCachedTokens + 10 {
		c.put(tokenKey(fmt.Sprintf("valid-%d", i)), verdict{info: info, expires: now.Add(time.Hour), generation: 2}, now)
	}
	assert.LessOrEqual(t, c.len(), maxCachedTokens)
}

func TestVerdicts_HoldForTheirGenerationOnly(t *testing.T) {
	c := newVerdicts()
	now := time.Now()
	c.put(tokenKey("token"), verdict{info: &sdkauth.TokenInfo{}, expires: now.Add(time.Hour), generation: 3}, now)
	_, ok := c.get(tokenKey("token"), 3, now)
	assert.True(t, ok)
	_, ok = c.get(tokenKey("token"), 4, now)
	assert.False(t, ok)
	_, ok = c.get(tokenKey("token"), 3, now.Add(2*time.Hour))
	assert.False(t, ok)
}
