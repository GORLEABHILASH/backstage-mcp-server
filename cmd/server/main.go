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
	"github.com/GORLEABHILASH/backstage-mcp-server/internal/tools"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	serverName    = "backstage-mcp-server"
	serverVersion = "0.1.0"
)

func main() {
	baseURL := flag.String("backstage-url", envOr("BACKSTAGE_URL", "http://localhost:7007"),
		"Base URL of the Backstage instance (env: BACKSTAGE_URL)")
	token := flag.String("backstage-token", os.Getenv("BACKSTAGE_TOKEN"),
		"Optional bearer token for Backstage (env: BACKSTAGE_TOKEN)")
	flag.Parse()

	// stderr logger — stdout is reserved for MCP protocol traffic.
	logger := log.New(os.Stderr, "[backstage-mcp] ", log.LstdFlags|log.Lmsgprefix)
	logger.Printf("starting %s v%s (backstage=%s)", serverName, serverVersion, *baseURL)

	client, err := backstage.New(backstage.Config{
		BaseURL: *baseURL,
		Token:   *token,
	})
	if err != nil {
		logger.Fatalf("init backstage client: %v", err)
	}

	server := mcp.NewServer(&mcp.Implementation{
		Name:    serverName,
		Version: serverVersion,
	}, nil)

	tools.Register(server, client)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil {
		logger.Fatalf("server exited: %v", err)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
