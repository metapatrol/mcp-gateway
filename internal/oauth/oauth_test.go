package oauth

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
)

func counting(expiresIn time.Duration) (Fetch, *atomic.Int64) {
	var n atomic.Int64
	return func(context.Context) (*oauth2.Token, error) {
		i := n.Add(1)
		return &oauth2.Token{AccessToken: fmt.Sprintf("token-%d", i), Expiry: time.Now().Add(expiresIn)}, nil
	}, &n
}

func TestCache_ReusesATokenUntilItIsAboutToExpire(t *testing.T) {
	fetch, n := counting(time.Hour)
	c := NewCache(fetch)
	for range 3 {
		token, err := c.Token(context.Background())
		require.NoError(t, err)
		assert.Equal(t, "token-1", token.AccessToken)
	}
	assert.Equal(t, int64(1), n.Load())

	fetch, n = counting(RefreshMargin / 2)
	c = NewCache(fetch)
	_, _ = c.Token(context.Background())
	_, _ = c.Token(context.Background())
	assert.Equal(t, int64(2), n.Load(), "a token inside the margin is not handed out")
}

func TestCache_InvalidateDropsOnlyTheRefusedToken(t *testing.T) {
	fetch, _ := counting(time.Hour)
	c := NewCache(fetch)
	first, err := c.Token(context.Background())
	require.NoError(t, err)

	c.Invalidate("some-other-token")
	again, _ := c.Token(context.Background())
	assert.Same(t, first, again)

	c.Invalidate(first.AccessToken)
	second, _ := c.Token(context.Background())
	assert.Equal(t, "token-2", second.AccessToken)
	c.Invalidate(first.AccessToken)
	third, _ := c.Token(context.Background())
	assert.Same(t, second, third, "a token another caller already replaced stays replaced")
}

// One caller giving up must not fail the fetch every other caller waits on.
func TestCache_TheFetchOutlivesTheCaller(t *testing.T) {
	c := NewCache(func(ctx context.Context) (*oauth2.Token, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return &oauth2.Token{AccessToken: "t"}, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	token, err := c.Token(ctx)
	require.NoError(t, err)
	assert.Equal(t, "t", token.AccessToken)
}
