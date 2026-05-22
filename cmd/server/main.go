// Command backstage-mcp-server runs an MCP server that exposes the Backstage
// Software Catalog and TechDocs RAG as MCP tools, consumable by LLM clients
// such as Claude Code, Cursor, and Red Hat Developer Hub.
//
// Two transports are supported:
//
//   - stdio: default; the server reads MCP requests from stdin and writes
//     responses to stdout. Use this for local Claude Code / Cursor integration.
//   - http: serves the Streamable HTTP transport plus /healthz on a TCP
//     listener. Use this when running inside Kubernetes / OpenShift.
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/GORLEABHILASH/backstage-mcp-server/internal/backstage"
	"github.com/GORLEABHILASH/backstage-mcp-server/internal/rag"
	"github.com/GORLEABHILASH/backstage-mcp-server/internal/tools"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const serverName = "backstage-mcp-server"

// serverVersion is overridable at build time via
// `-ldflags "-X main.serverVersion=<version>"`.
var serverVersion = "0.3.0"

func main() {
	transport := flag.String("transport", envOr("MCP_TRANSPORT", "stdio"),
		"Transport: stdio | http (env: MCP_TRANSPORT)")
	httpAddr := flag.String("http-addr", envOr("HTTP_ADDR", ":8080"),
		"Address to listen on when --transport=http (env: HTTP_ADDR)")
	httpPath := flag.String("http-path", envOr("HTTP_PATH", "/mcp"),
		"URL path for the MCP endpoint when --transport=http (env: HTTP_PATH)")

	backstageURL := flag.String("backstage-url", envOr("BACKSTAGE_URL", "http://localhost:7007"),
		"Base URL of the Backstage instance (env: BACKSTAGE_URL)")
	backstageToken := flag.String("backstage-token", os.Getenv("BACKSTAGE_TOKEN"),
		"Optional bearer token for Backstage (env: BACKSTAGE_TOKEN)")

	chromaURL := flag.String("chroma-url", os.Getenv("CHROMA_URL"),
		"Base URL of ChromaDB (env: CHROMA_URL). Leave empty to disable query_docs.")
	chromaCollection := flag.String("chroma-collection", envOr("CHROMA_COLLECTION", "backstage_techdocs"),
		"Chroma collection name (env: CHROMA_COLLECTION)")
	openaiKey := flag.String("openai-key", os.Getenv("OPENAI_API_KEY"),
		"OpenAI API key for embeddings (env: OPENAI_API_KEY). Required when --chroma-url is set.")
	embedModel := flag.String("embed-model", envOr("EMBED_MODEL", "text-embedding-3-small"),
		"OpenAI embedding model (env: EMBED_MODEL)")

	flag.Parse()

	// HTTP mode logs to stderr (stdout is fine too, but we keep them consistent).
	// stdio mode MUST log to stderr — stdout is reserved for the protocol stream.
	logger := log.New(os.Stderr, "[backstage-mcp] ", log.LstdFlags|log.Lmsgprefix)
	logger.Printf("starting %s v%s (transport=%s backstage=%s)", serverName, serverVersion, *transport, *backstageURL)

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

	newServer := func() *mcp.Server {
		s := mcp.NewServer(&mcp.Implementation{
			Name:    serverName,
			Version: serverVersion,
		}, nil)
		tools.Register(s, deps)
		return s
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	switch *transport {
	case "stdio":
		s := newServer()
		if err := s.Run(ctx, &mcp.StdioTransport{}); err != nil {
			logger.Fatalf("server exited: %v", err)
		}
	case "http":
		if err := runHTTP(ctx, logger, *httpAddr, *httpPath, newServer); err != nil {
			logger.Fatalf("http server exited: %v", err)
		}
	default:
		logger.Fatalf("unknown --transport %q (want stdio|http)", *transport)
	}
}

// runHTTP serves the MCP Streamable HTTP handler plus /healthz on the given
// address. It returns when ctx is cancelled or the listener fails.
func runHTTP(ctx context.Context, logger *log.Logger, addr, path string, newServer func() *mcp.Server) error {
	mcpHandler := mcp.NewStreamableHTTPHandler(func(_ *http.Request) *mcp.Server {
		// One server per request: stateless, safe for replicas behind a Service.
		return newServer()
	}, nil)

	mux := http.NewServeMux()
	mux.Handle(path, mcpHandler)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready"))
	})

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		<-ctx.Done()
		logger.Printf("shutting down http server")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	logger.Printf("listening on %s (mcp=%s healthz=/healthz)", addr, path)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
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
