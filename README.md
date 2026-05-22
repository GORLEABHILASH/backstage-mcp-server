# backstage-mcp-server

An [MCP (Model Context Protocol)](https://modelcontextprotocol.io) server, written in **Go**, that exposes the [Backstage](https://backstage.io) Software Catalog to LLM clients such as **Claude Code**, **Cursor**, and **Red Hat Developer Hub** agents.

> Backstage is the open-source platform underlying [Red Hat Developer Hub](https://developers.redhat.com/products/developer-hub/overview). This project lets an LLM answer questions like *"which services use Postgres?"* or *"who owns the payments API?"* by calling typed MCP tools against a live catalog instead of guessing.

## Status

| Phase | Scope | Status |
|---|---|---|
| 1 | Go MCP server, stdio transport, `search_catalog` + `get_entity` tools | In progress |
| 2 | RAG over Backstage TechDocs (ChromaDB) — `query_docs` tool | Planned |
| 3 | Container image, Helm chart, deploy on OpenShift Local / kind | Planned |
| 4 | Tekton CI pipeline + Argo CD GitOps deploy | Planned |
| 5 | Upstream contribution to Janus IDP / Backstage Community Plugins | Planned |

## Architecture

```
+----------------+   stdio (MCP)    +--------------------+   HTTPS    +------------------+
|  Claude Code   | <--------------> | backstage-mcp-     | <--------> | Backstage        |
|  / Cursor / DH |   JSON-RPC 2.0   | server  (Go)       |   REST     | Software Catalog |
+----------------+                  +--------------------+            +------------------+
                                            |
                                            +--> tool: search_catalog
                                            +--> tool: get_entity
                                            +--> tool: query_docs   (phase 2, RAG)
```

The server is a single Go binary. It speaks MCP over stdio (and later HTTP/SSE) and translates each tool call into a Backstage REST request.

## Tools

| Name | Purpose | Inputs |
|---|---|---|
| `search_catalog` | Discover entities in the catalog | `kind` (optional), `query` (optional), `limit` (optional) |
| `get_entity` | Fetch full details for one entity | `ref` (e.g. `component:default/payments`) |

## Quickstart

### Prerequisites
- Go 1.25+
- A reachable Backstage instance (defaults to `http://localhost:7007`)

### Build
```bash
make build
```

### Run standalone (for debugging)
```bash
./bin/backstage-mcp-server --backstage-url=http://localhost:7007
```
The server listens on stdio and waits for an MCP client.

### Use with Claude Code
1. Build the binary: `make build`
2. Copy `.mcp.json.example` to `.mcp.json` and update the absolute path
3. Restart Claude Code — `search_catalog` and `get_entity` will appear in the tool list

### Inspect with the MCP Inspector
```bash
make inspect
```
Opens the official MCP Inspector UI in your browser and connects it to the local binary — useful for poking at tools without an LLM.

## Configuration

| Flag | Env var | Default | Description |
|---|---|---|---|
| `--backstage-url` | `BACKSTAGE_URL` | `http://localhost:7007` | Base URL of the Backstage instance |
| `--backstage-token` | `BACKSTAGE_TOKEN` | *(empty)* | Bearer token for protected catalogs |

## Repo layout

```
.
├── cmd/server/          # main.go — binary entry point
├── internal/
│   ├── backstage/       # thin Software Catalog REST client
│   └── tools/           # MCP tool definitions and handlers
├── deploy/              # (phase 3+) Helm chart, OpenShift manifests
├── docs/                # (phase 2+) ingestion + RAG notes
├── Makefile
└── .mcp.json.example
```

## Development

```bash
make tidy   # go mod tidy
make fmt    # gofmt -s -w .
make vet    # go vet ./...
make test   # go test ./...
```

## Why this exists

Red Hat Developer Hub builds on Backstage and is investing in AI assistants and MCP servers for the platform. Most existing MCP servers target generic data sources (filesystem, git, databases). This project closes the gap between an LLM and a developer platform's catalog so an engineer can ask natural-language questions about their own infrastructure and get grounded answers.

## License

[Apache 2.0](./LICENSE)
