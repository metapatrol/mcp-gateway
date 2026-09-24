package inspect

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"forgejo.metapatrol.local/platform/mcp-gateway/internal/discovery"
	"forgejo.metapatrol.local/platform/mcp-gateway/internal/inspect/api"
	"forgejo.metapatrol.local/platform/mcp-gateway/internal/policy"
)

// The tools/list documents: what a resource hands out, written as an MCP
// client receives it. Each goes through the SDK's own JSON encoding of a tool,
// the one the gateway's tools/list answers are written with.

func toolsList(tools []*mcp.Tool) (api.ToolsList, error) {
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	out := api.ToolsList{Tools: make([]map[string]any, 0, len(tools))}
	for _, t := range tools {
		b, err := json.Marshal(t)
		if err != nil {
			return out, err
		}
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			return out, err
		}
		out.Tools = append(out.Tools, m)
	}
	return out, nil
}

// served returns the tools a resolution comes to that the gateway offers right
// now, under their exposed names. Conflicts are not in a resolution's Tools.
func (s *Server) served(res policy.Resolution) []*mcp.Tool {
	var out []*mcp.Tool
	for name, ref := range res.Tools {
		c, ok := s.Gateway.UsableCatalog(ref.Server)
		if !ok {
			continue
		}
		if t, ok := c.Tool(ref.Tool); ok {
			out = append(out, discovery.Exposed(t, name))
		}
	}
	return out
}

func (s *Server) ListServerTools(ctx context.Context, req api.ListServerToolsRequestObject) (api.ListServerToolsResponseObject, error) {
	w, err := s.load(ctx)
	if err != nil {
		return nil, err
	}
	if _, ok := w.server(req.Name); !ok {
		return api.ListServerTools404ApplicationProblemPlusJSONResponse{NotFoundApplicationProblemPlusJSONResponse: notFound("MCPServer", req.Name)}, nil
	}
	var tools []*mcp.Tool
	if c, _, ok := s.Registry.Catalog(req.Name); ok {
		for _, name := range c.Names() {
			t, _ := c.Tool(name)
			tools = append(tools, t)
		}
	}
	doc, err := toolsList(tools)
	return api.ListServerTools200JSONResponse(doc), err
}

func (s *Server) ListToolSetTools(ctx context.Context, req api.ListToolSetToolsRequestObject) (api.ListToolSetToolsResponseObject, error) {
	w, err := s.load(ctx)
	if err != nil {
		return nil, err
	}
	if _, ok := w.toolSet(req.Name); !ok {
		return api.ListToolSetTools404ApplicationProblemPlusJSONResponse{NotFoundApplicationProblemPlusJSONResponse: notFound("MCPToolSet", req.Name)}, nil
	}
	doc, err := toolsList(s.served(s.Gateway.Policy().ResolveToolSets([]string{req.Name}, s.catalogs())))
	return api.ListToolSetTools200JSONResponse(doc), err
}

func (s *Server) ListCapabilityTools(ctx context.Context, req api.ListCapabilityToolsRequestObject) (api.ListCapabilityToolsResponseObject, error) {
	w, err := s.load(ctx)
	if err != nil {
		return nil, err
	}
	if _, ok := w.capability(req.Name); !ok {
		return api.ListCapabilityTools404ApplicationProblemPlusJSONResponse{NotFoundApplicationProblemPlusJSONResponse: notFound("MCPCapability", req.Name)}, nil
	}
	doc, err := toolsList(s.served(s.Gateway.Policy().ResolveCapabilities([]string{req.Name}, s.catalogs())))
	return api.ListCapabilityTools200JSONResponse(doc), err
}

func (s *Server) ListGrantTools(ctx context.Context, req api.ListGrantToolsRequestObject) (api.ListGrantToolsResponseObject, error) {
	w, err := s.load(ctx)
	if err != nil {
		return nil, err
	}
	g, ok := w.grant(req.Name)
	if !ok {
		return api.ListGrantTools404ApplicationProblemPlusJSONResponse{NotFoundApplicationProblemPlusJSONResponse: notFound("MCPGrant", req.Name)}, nil
	}
	doc, err := toolsList(s.served(s.Gateway.Policy().ResolveCapabilities(g.Spec.Capabilities, s.catalogs())))
	return api.ListGrantTools200JSONResponse(doc), err
}
