package inspect

import (
	"context"
	"encoding/json"

	"forgejo.metapatrol.local/platform/mcp-gateway/internal/inspect/api"
	"forgejo.metapatrol.local/platform/mcp-gateway/internal/policy"
)

// The MCP views: what an agent holding a resource receives, and what a
// backend answered the gateway. The agent's side is answered by the gateway
// itself (Gateway.Preview), through the server and handler agents reach, so it
// is what an agent receives and not a rendering of it.

func agentExchange(ctx context.Context, gw Gateway, res policy.Resolution) (api.AgentExchange, error) {
	p, err := gw.Preview(ctx, res)
	if err != nil {
		return api.AgentExchange{}, err
	}
	return api.AgentExchange{Discover: p.Discover, ToolsList: p.ToolsList}, nil
}

func (s *Server) GetServerMCP(ctx context.Context, req api.GetServerMCPRequestObject) (api.GetServerMCPResponseObject, error) {
	w, err := s.load(ctx)
	if err != nil {
		return nil, err
	}
	if _, ok := w.server(req.Name); !ok {
		return api.GetServerMCP404ApplicationProblemPlusJSONResponse{NotFoundApplicationProblemPlusJSONResponse: notFound("MCPServer", req.Name)}, nil
	}
	out := api.BackendExchange{ToolsList: []json.RawMessage{}}
	c, _, ok := s.Registry.Catalog(req.Name)
	if !ok {
		return api.GetServerMCP200JSONResponse(out), nil
	}
	if c.Handshake != nil {
		b, err := json.Marshal(c.Handshake)
		if err != nil {
			return nil, err
		}
		raw := json.RawMessage(b)
		out.Handshake = &raw
	}
	for _, page := range c.Pages {
		b, err := json.Marshal(page)
		if err != nil {
			return nil, err
		}
		out.ToolsList = append(out.ToolsList, b)
	}
	return api.GetServerMCP200JSONResponse(out), nil
}

func (s *Server) GetToolSetMCP(ctx context.Context, req api.GetToolSetMCPRequestObject) (api.GetToolSetMCPResponseObject, error) {
	w, err := s.load(ctx)
	if err != nil {
		return nil, err
	}
	if _, ok := w.toolSet(req.Name); !ok {
		return api.GetToolSetMCP404ApplicationProblemPlusJSONResponse{NotFoundApplicationProblemPlusJSONResponse: notFound("MCPToolSet", req.Name)}, nil
	}
	ex, err := agentExchange(ctx, s.Gateway, s.Gateway.Policy().ResolveToolSets([]string{req.Name}, s.catalogs()))
	return api.GetToolSetMCP200JSONResponse(ex), err
}

func (s *Server) GetCapabilityMCP(ctx context.Context, req api.GetCapabilityMCPRequestObject) (api.GetCapabilityMCPResponseObject, error) {
	w, err := s.load(ctx)
	if err != nil {
		return nil, err
	}
	if _, ok := w.capability(req.Name); !ok {
		return api.GetCapabilityMCP404ApplicationProblemPlusJSONResponse{NotFoundApplicationProblemPlusJSONResponse: notFound("MCPCapability", req.Name)}, nil
	}
	ex, err := agentExchange(ctx, s.Gateway, s.Gateway.Policy().ResolveCapabilities([]string{req.Name}, s.catalogs()))
	return api.GetCapabilityMCP200JSONResponse(ex), err
}

func (s *Server) GetGrantMCP(ctx context.Context, req api.GetGrantMCPRequestObject) (api.GetGrantMCPResponseObject, error) {
	w, err := s.load(ctx)
	if err != nil {
		return nil, err
	}
	g, ok := w.grant(req.Name)
	if !ok {
		return api.GetGrantMCP404ApplicationProblemPlusJSONResponse{NotFoundApplicationProblemPlusJSONResponse: notFound("MCPGrant", req.Name)}, nil
	}
	ex, err := agentExchange(ctx, s.Gateway, s.Gateway.Policy().ResolveCapabilities(g.Spec.Capabilities, s.catalogs()))
	return api.GetGrantMCP200JSONResponse(ex), err
}

func (s *Server) GetSubjectMCP(ctx context.Context, req api.GetSubjectMCPRequestObject) (api.GetSubjectMCPResponseObject, error) {
	ex, err := agentExchange(ctx, s.Gateway, s.Gateway.Policy().Resolve(req.Params.Subject, s.catalogs()))
	return api.GetSubjectMCP200JSONResponse(ex), err
}
