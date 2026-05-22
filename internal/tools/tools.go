// Package tools defines the MCP tools exposed by the server.
//
// Each tool maps to a typed handler. Input/output schemas are inferred from
// the Go structs by the MCP SDK, so adding a new field with a `jsonschema`
// tag is enough to surface it to the LLM.
package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/GORLEABHILASH/backstage-mcp-server/internal/backstage"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Register wires every tool in this package onto the given server.
func Register(server *mcp.Server, client *backstage.Client) {
	registerSearchCatalog(server, client)
	registerGetEntity(server, client)
}

// --- search_catalog -----------------------------------------------------

type searchCatalogArgs struct {
	Kind  string `json:"kind,omitempty" jsonschema:"optional Backstage kind filter (Component, API, System, Resource, Group, User)"`
	Query string `json:"query,omitempty" jsonschema:"free-text search against entity name, title and description"`
	Limit int    `json:"limit,omitempty" jsonschema:"maximum number of entities to return (default 25)"`
}

func registerSearchCatalog(server *mcp.Server, client *backstage.Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "search_catalog",
		Description: "Search the Backstage Software Catalog for entities (components, APIs, systems, etc). Use this to discover what services exist in the developer platform.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args searchCatalogArgs) (*mcp.CallToolResult, any, error) {
		entities, err := client.Search(ctx, backstage.SearchOptions{
			Kind:  args.Kind,
			Query: args.Query,
			Limit: args.Limit,
		})
		if err != nil {
			return errorResult(err), nil, nil
		}
		return jsonResult(entities), nil, nil
	})
}

// --- get_entity ---------------------------------------------------------

type getEntityArgs struct {
	Ref string `json:"ref" jsonschema:"Backstage entity ref in the form kind:namespace/name (e.g. component:default/payments)"`
}

func registerGetEntity(server *mcp.Server, client *backstage.Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_entity",
		Description: "Fetch full metadata, spec, and relations for a single Backstage entity by ref. Use this after search_catalog to drill into a specific service.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args getEntityArgs) (*mcp.CallToolResult, any, error) {
		if args.Ref == "" {
			return errorResult(fmt.Errorf("ref is required")), nil, nil
		}
		entity, err := client.GetByRef(ctx, args.Ref)
		if err != nil {
			return errorResult(err), nil, nil
		}
		return jsonResult(entity), nil, nil
	})
}

// --- helpers ------------------------------------------------------------

func jsonResult(v any) *mcp.CallToolResult {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return errorResult(fmt.Errorf("marshal result: %w", err))
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(b)}},
	}
}

func errorResult(err error) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
	}
}
