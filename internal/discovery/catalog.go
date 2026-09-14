// Package discovery talks MCP to the backends: it holds one client session per
// MCPServer, reads their tool catalogs, and forwards tool calls.
//
// The controller and the gateway both use it and neither depends on the other
// for it. The controller reads catalogs to report status; the gateway reads
// them to serve tools/list and route tools/call. Discovering twice is the price
// of keeping the controller out of the request path, and it is cheap: one
// tools/list per backend per interval.
package discovery

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Catalog is the tool list one backend exposed at one moment.
//
// A Catalog is never modified once built. A rediscovery that finds the same
// revision keeps the old one, which is what lets the gateway compare catalogs by
// pointer and notify connected agents only when something actually changed.
type Catalog struct {
	// Revision is a sha256 over the canonical JSON of the tools, sorted by
	// name.
	Revision string

	tools map[string]*mcp.Tool
	names []string
}

// NewCatalog builds a catalog. When a backend lists a name twice, the first
// definition wins: the backend is misbehaving, and a stable choice is better
// than one that flips between discoveries.
func NewCatalog(tools []*mcp.Tool) *Catalog {
	c := &Catalog{tools: make(map[string]*mcp.Tool, len(tools))}
	for _, t := range tools {
		if t == nil {
			continue
		}
		if _, seen := c.tools[t.Name]; seen {
			continue
		}
		c.tools[t.Name] = t
		c.names = append(c.names, t.Name)
	}
	sort.Strings(c.names)
	c.Revision = revision(c.names, c.tools)
	return c
}

// Tool returns the named tool.
func (c *Catalog) Tool(name string) (*mcp.Tool, bool) {
	t, ok := c.tools[name]
	return t, ok
}

// Names returns the tool names, sorted. The slice must not be modified.
func (c *Catalog) Names() []string { return c.names }

// Len is the number of tools.
func (c *Catalog) Len() int { return len(c.names) }

// revision hashes the tools in name order. encoding/json sorts map keys, so a
// schema that arrives with its properties in another order hashes the same.
func revision(names []string, tools map[string]*mcp.Tool) string {
	h := sha256.New()
	for _, name := range names {
		b, err := json.Marshal(tools[name])
		if err != nil {
			// A tool that decoded from JSON re-encodes; this is unreachable
			// short of a bug in the SDK. Hash the name alone rather than fail
			// the discovery over it.
			b = []byte(name)
		}
		h.Write(b)
		h.Write([]byte{'\n'})
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}
