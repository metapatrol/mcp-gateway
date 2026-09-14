package auth

import (
	"crypto/sha256"
	"sync"
	"time"

	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
)

// verdicts remembers what verifying a token came to, keyed by a hash of the
// token and never the token itself.
//
// A verdict holds until it expires, and only for the generation it was reached
// under: the introspector has one generation, the JWT verifier one per version
// of the provider's key set, so that a key the provider withdraws stops
// vouching for the tokens it signed as soon as the gateway sees it withdrawn.
type verdicts struct {
	mu      sync.Mutex
	entries map[[sha256.Size]byte]verdict
}

type verdict struct {
	info       *sdkauth.TokenInfo
	err        error
	expires    time.Time
	generation uint64
}

// maxCachedTokens bounds the cache. Past it, expired entries are swept, and if
// that does not help the cache starts over — crude, and enough: the bound
// exists to stop a flood of junk tokens from growing memory, not to be an LRU.
const maxCachedTokens = 10_000

func newVerdicts() *verdicts {
	return &verdicts{entries: map[[sha256.Size]byte]verdict{}}
}

func tokenKey(token string) [sha256.Size]byte { return sha256.Sum256([]byte(token)) }

// get returns the verdict on a token, if one holds at now for generation.
func (c *verdicts) get(key [sha256.Size]byte, generation uint64, now time.Time) (verdict, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.entries[key]
	if !ok || !now.Before(v.expires) || v.generation != generation {
		return verdict{}, false
	}
	return v, true
}

// put records a verdict.
func (c *verdicts) put(key [sha256.Size]byte, v verdict, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) >= maxCachedTokens {
		for k, e := range c.entries {
			if !now.Before(e.expires) || e.generation != v.generation {
				delete(c.entries, k)
			}
		}
		if len(c.entries) >= maxCachedTokens {
			c.entries = map[[sha256.Size]byte]verdict{}
		}
	}
	c.entries[key] = v
}

func (c *verdicts) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}
