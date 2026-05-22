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
	"github.com/GORLEABHILASH/backstage-mcp-server/internal/rag"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Deps bundles the optional collaborators the tools depend on. The RAG store
// is nil-safe: when nil, the query_docs tool is simply not registered.
type Deps struct {
	Backstage *backstage.Client
	RAG       *rag.Store
}

// Register wires every available tool onto the given server. Tools whose
// dependencies are missing are skipped silently so the server still runs
// with a partial feature set (useful for local dev without ChromaDB).
func Register(server *mcp.Server, deps Deps) {
	if deps.Backstage != nil {
		registerSearchCatalog(server, deps.Backstage)
		registerGetEntity(server, deps.Backstage)
	}
	if deps.RAG != nil {
		registerQueryDocs(server, deps.RAG)
	}
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

// --- query_docs ---------------------------------------------------------

type queryDocsArgs struct {
	Query string `json:"query" jsonschema:"natural language question to ask against the indexed TechDocs"`
	K     int    `json:"k,omitempty" jsonschema:"number of chunks to retrieve (default 5)"`
}

func registerQueryDocs(server *mcp.Server, store *rag.Store) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "query_docs",
		Description: "Semantic search over indexed Backstage TechDocs. Returns the top-k chunks most relevant to the query, with source attribution. Use this when search_catalog metadata is not enough and you need actual documentation content.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args queryDocsArgs) (*mcp.CallToolResult, any, error) {
		if args.Query == "" {
			return errorResult(fmt.Errorf("query is required")), nil, nil
		}
		hits, err := store.Query(ctx, args.Query, args.K)
		if err != nil {
			return errorResult(err), nil, nil
		}
		return jsonResult(hits), nil, nil
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
