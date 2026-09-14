// Package oauth keeps an OAuth access token fresh for a client: the proxy's own
// token towards the gateway, and the gateway's tokens towards backends that want
// client credentials.
package oauth

import (
	"context"
	"net/http"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
)

// Fetch obtains a new token. The cache calls it only when it holds none it
// would still hand out.
type Fetch func(ctx context.Context) (*oauth2.Token, error)

// ClientCredentials fetches with the client credentials grant.
func ClientCredentials(cfg *clientcredentials.Config, httpClient *http.Client) Fetch {
	return func(ctx context.Context) (*oauth2.Token, error) {
		return cfg.Token(context.WithValue(ctx, oauth2.HTTPClient, httpClient))
	}
}

// RefreshMargin is how long before expiry a token is replaced. It covers the
// time a request spends in flight plus modest clock skew.
const RefreshMargin = 30 * time.Second

// Cache holds the current token of one client.
//
// A token is replaced shortly before it expires, and dropped when the party it
// was presented to refuses it: a token can be revoked, or a clock can disagree,
// long before its expiry says so.
type Cache struct {
	fetch Fetch

	mu    sync.Mutex
	token *oauth2.Token
}

// NewCache creates an empty cache.
func NewCache(fetch Fetch) *Cache {
	return &Cache{fetch: fetch}
}

// Token returns the cached token, fetching a new one when there is none or it is
// about to expire.
//
// The fetch runs detached from the caller's cancellation, bounded at 30s:
// concurrent callers wait on the same lock for the same token, and one of them
// giving up must not fail it for the others.
func (c *Cache) Token(ctx context.Context) (*oauth2.Token, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != nil && (c.token.Expiry.IsZero() || time.Until(c.token.Expiry) > RefreshMargin) {
		return c.token, nil
	}
	fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	token, err := c.fetch(fetchCtx)
	if err != nil {
		return nil, err
	}
	c.token = token
	return token, nil
}

// Invalidate drops the token whose access token was refused, if it is still the
// current one. A token another caller already replaced stays replaced.
func (c *Cache) Invalidate(accessToken string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != nil && c.token.AccessToken == accessToken {
		c.token = nil
	}
}
