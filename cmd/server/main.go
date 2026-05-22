// Command backstage-mcp-server runs an MCP server that exposes the Backstage
// Software Catalog as MCP tools, consumable by LLM clients such as Claude
// Code or Cursor.
//
// Transport: stdio. The server reads MCP requests from stdin and writes
// responses to stdout; logs go to stderr so they do not corrupt the
// protocol stream.
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/GORLEABHILASH/backstage-mcp-server/internal/backstage"
	"github.com/GORLEABHILASH/backstage-mcp-server/internal/rag"
	"github.com/GORLEABHILASH/backstage-mcp-server/internal/tools"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	serverName    = "backstage-mcp-server"
	serverVersion = "0.2.0"
)

func main() {
	backstageURL := flag.String("backstage-url", envOr("BACKSTAGE_URL", "http://localhost:7007"),
		"Base URL of the Backstage instance (env: BACKSTAGE_URL)")
	backstageToken := flag.String("backstage-token", os.Getenv("BACKSTAGE_TOKEN"),
		"Optional bearer token for Backstage (env: BACKSTAGE_TOKEN)")

	// RAG is opt-in: provide a Chroma URL + OpenAI key to enable query_docs.
	chromaURL := flag.String("chroma-url", os.Getenv("CHROMA_URL"),
		"Base URL of ChromaDB (env: CHROMA_URL). Leave empty to disable query_docs.")
	chromaCollection := flag.String("chroma-collection", envOr("CHROMA_COLLECTION", "backstage_techdocs"),
		"Chroma collection name (env: CHROMA_COLLECTION)")
	openaiKey := flag.String("openai-key", os.Getenv("OPENAI_API_KEY"),
		"OpenAI API key for embeddings (env: OPENAI_API_KEY). Required when --chroma-url is set.")
	embedModel := flag.String("embed-model", envOr("EMBED_MODEL", "text-embedding-3-small"),
		"OpenAI embedding model (env: EMBED_MODEL)")

	flag.Parse()

	// stderr logger — stdout is reserved for MCP protocol traffic.
	logger := log.New(os.Stderr, "[backstage-mcp] ", log.LstdFlags|log.Lmsgprefix)
	logger.Printf("starting %s v%s (backstage=%s)", serverName, serverVersion, *backstageURL)

	bsClient, err := backstage.New(backstage.Config{
		BaseURL: *backstageURL,
		Token:   *backstageToken,
	})
	if err != nil {
		logger.Fatalf("init backstage client: %v", err)
	}

	deps := tools.Deps{Backstage: bsClient}

	if *chromaURL != "" {
		store, err := buildRAGStore(*chromaURL, *chromaCollection, *openaiKey, *embedModel)
		if err != nil {
			logger.Fatalf("init RAG store: %v", err)
		}
		deps.RAG = store
		logger.Printf("RAG enabled (chroma=%s collection=%s embed-model=%s)", *chromaURL, *chromaCollection, *embedModel)
	} else {
		logger.Printf("RAG disabled — query_docs tool will not be registered (set --chroma-url to enable)")
	}

	server := mcp.NewServer(&mcp.Implementation{
		Name:    serverName,
		Version: serverVersion,
	}, nil)

	tools.Register(server, deps)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil {
		logger.Fatalf("server exited: %v", err)
	}
}

func buildRAGStore(chromaURL, collection, openaiKey, model string) (*rag.Store, error) {
	chroma, err := rag.NewChromaClient(rag.ChromaConfig{BaseURL: chromaURL})
	if err != nil {
		return nil, err
	}
	embedder, err := rag.NewOpenAIEmbedder(rag.OpenAIConfig{
		APIKey: openaiKey,
		Model:  model,
	})
	if err != nil {
		return nil, err
	}
	return rag.NewStore(rag.StoreConfig{
		Chroma:     chroma,
		Embedder:   embedder,
		Collection: collection,
	})
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
