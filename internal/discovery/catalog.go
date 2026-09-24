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

// Catalog is what one backend offered at one moment: its tools, and the
// instructions it gives clients on how to use them.
//
// A Catalog is never modified once built. A rediscovery that finds the same
// revision keeps the old one, which is what lets the gateway compare catalogs by
// pointer and notify connected agents only when something actually changed.
type Catalog struct {
	// Revision is a sha256 over the canonical JSON of the tools, sorted by
	// name, followed by the instructions.
	Revision string

	// Instructions is what the backend returned as instructions when the
	// session was opened. Empty when it gave none.
	Instructions string

	tools map[string]*mcp.Tool
	names []string
}

// NewCatalog builds a catalog. When a backend lists a name twice, the first
// definition wins: the backend is misbehaving, and a stable choice is better
// than one that flips between discoveries.
func NewCatalog(tools []*mcp.Tool, instructions string) *Catalog {
	c := &Catalog{Instructions: instructions, tools: make(map[string]*mcp.Tool, len(tools))}
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
	c.Revision = revision(c.names, c.tools, instructions)
	return c
}

// Tool returns the named tool.
func (c *Catalog) Tool(name string) (*mcp.Tool, bool) {
	t, ok := c.tools[name]
	return t, ok
}

// Exposed is a backend's tool as it is offered under name: the backend's
// definition unchanged, but for the name. It is how the gateway registers a
// tool for agents, and how the inspect API shows one, so the two cannot
// differ.
func Exposed(tool *mcp.Tool, name string) *mcp.Tool {
	t := *tool
	t.Name = name
	return &t
}

// Names returns the tool names, sorted. The slice must not be modified.
func (c *Catalog) Names() []string { return c.names }

// Len is the number of tools.
func (c *Catalog) Len() int { return len(c.names) }

// revision hashes the tools in name order, then the instructions. encoding/json
// sorts map keys, so a schema that arrives with its properties in another order
// hashes the same.
//
// The instructions are part of it because they are part of what the backend
// offers: a backend redeployed with a new usage note and the same tools has
// still changed, and the gateway has to pass the new note on.
func revision(names []string, tools map[string]*mcp.Tool, instructions string) string {
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
	h.Write([]byte{0})
	h.Write([]byte(instructions))
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}
