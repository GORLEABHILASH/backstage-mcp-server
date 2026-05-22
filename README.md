# backstage-mcp-server

An [MCP (Model Context Protocol)](https://modelcontextprotocol.io) server, written in **Go**, that exposes the [Backstage](https://backstage.io) Software Catalog and TechDocs to LLM clients such as **Claude Code**, **Cursor**, and **Red Hat Developer Hub** agents.

> Backstage is the open-source platform underlying [Red Hat Developer Hub](https://developers.redhat.com/products/developer-hub/overview). This project lets an LLM answer questions like *"which services use Postgres?"* or *"how do I roll back a Payments deploy?"* by calling typed MCP tools — backed by the live catalog and a RAG index over TechDocs — instead of guessing.

## Status

| Phase | Scope | Status |
|---|---|---|
| 1 | Go MCP server, stdio transport, `search_catalog` + `get_entity` | Done |
| 2 | RAG over Backstage TechDocs (ChromaDB) — `query_docs` tool + indexer | Done |
| 3 | HTTP transport, distroless container, Helm chart, OpenShift Route | Done |
| 4 | Tekton CI pipeline + Argo CD GitOps deploy | Planned |
| 5 | Upstream contribution to Janus IDP / Backstage Community Plugins | Planned |

## Architecture

```
+----------------+   stdio (MCP)    +--------------------+   HTTPS    +------------------+
|  Claude Code   | <--------------> | backstage-mcp-     | <--------> | Backstage        |
|  / Cursor / DH |   JSON-RPC 2.0   | server  (Go)       |   REST     | Software Catalog |
+----------------+                  +--------------------+            +------------------+
                                            |   ^
                                            |   | embed query + ANN search
                                            v   |
                                       +------------+         +-------------+
                                       |  ChromaDB  | <-----  |  OpenAI     |
                                       |  (vector)  |         |  embeddings |
                                       +------------+         +-------------+
                                            ^
                                            | indexer (offline)
                                            |
                                       +-----------------+
                                       | TechDocs index  |  (MkDocs search_index.json)
                                       | or local files  |
                                       +-----------------+
```

The server is a single Go binary (`backstage-mcp-server`). A companion binary (`backstage-mcp-indexer`) walks documents and writes embeddings into ChromaDB. Both share the same `internal/rag` package.

## Tools

| Name | Purpose | Inputs |
|---|---|---|
| `search_catalog` | Discover entities in the catalog | `kind?`, `query?`, `limit?` |
| `get_entity` | Fetch full details for one entity | `ref` (e.g. `component:default/payments`) |
| `query_docs` | Semantic search over indexed TechDocs | `query`, `k?` |

`query_docs` is registered only when the server is started with `--chroma-url` and `--openai-key` (or the matching env vars). Without them the server still runs and the catalog tools work — the RAG tool simply isn't advertised.

## Transports

| Transport | When to use | How to enable |
|---|---|---|
| `stdio` *(default)* | Local Claude Code / Cursor integration | run the binary; client launches it |
| `http` (Streamable HTTP) | Kubernetes / OpenShift / shared remote | `--transport=http --http-addr=:8080` |

In HTTP mode the server also exposes `/healthz` and `/readyz` for K8s probes; the MCP endpoint defaults to `/mcp`.

## Quickstart

### Prerequisites
- Go 1.25+
- Docker (for the local ChromaDB)
- An OpenAI API key, for embeddings

### 1. Build
```bash
make build       # builds bin/backstage-mcp-server and bin/backstage-mcp-indexer
```

### 2. Start ChromaDB locally
```bash
make chroma-up   # docker compose up -d on Chroma at :8000
```

### 3. Index the bundled sample docs
The repo ships a tiny `docs/sample/` tree so you can try RAG end-to-end without
a real Backstage instance.
```bash
export OPENAI_API_KEY=sk-...
make index-sample
```

### 4. (Optional) Index real Backstage TechDocs
```bash
./bin/backstage-mcp-indexer \
  --source=techdocs \
  --backstage-url=http://localhost:7007 \
  --refs=component:default/payments,component:default/users
```

### 5. Run the server
```bash
./bin/backstage-mcp-server \
  --backstage-url=http://localhost:7007 \
  --chroma-url=http://localhost:8000 \
  --openai-key=$OPENAI_API_KEY
```
The server listens on stdio and waits for an MCP client.

### 6. Wire it into Claude Code
1. Copy `.mcp.json.example` to `.mcp.json` and fill in the absolute path + your OpenAI key
2. Restart Claude Code — `search_catalog`, `get_entity`, and `query_docs` will appear in the tool list

### Poke at it with the MCP Inspector
```bash
make inspect
```
Opens the official MCP Inspector UI in your browser and connects to the local binary.

## Configuration

### Server (`backstage-mcp-server`)

| Flag | Env var | Default | Description |
|---|---|---|---|
| `--backstage-url` | `BACKSTAGE_URL` | `http://localhost:7007` | Base URL of the Backstage instance |
| `--backstage-token` | `BACKSTAGE_TOKEN` | *(empty)* | Bearer token for protected catalogs |
| `--chroma-url` | `CHROMA_URL` | *(empty — disables RAG)* | ChromaDB base URL |
| `--chroma-collection` | `CHROMA_COLLECTION` | `backstage_techdocs` | Collection name |
| `--openai-key` | `OPENAI_API_KEY` | *(empty)* | Required when RAG is enabled |
| `--embed-model` | `EMBED_MODEL` | `text-embedding-3-small` | OpenAI embedding model |

### Indexer (`backstage-mcp-indexer`)

| Flag | Default | Description |
|---|---|---|
| `--source` | `files` | `files` or `techdocs` |
| `--path` | `./docs` | Directory to walk (files mode) |
| `--exts` | `.md,.markdown,.txt` | File extensions to include |
| `--backstage-url` | *(env)* | Backstage URL (techdocs mode) |
| `--refs` | *(empty)* | Comma-separated entity refs to index (techdocs mode) |
| `--chroma-url` | `http://localhost:8000` | ChromaDB URL |
| `--chroma-collection` | `backstage_techdocs` | Target collection |
| `--openai-key` | *(env)* | Required |
| `--embed-model` | `text-embedding-3-small` | Embedding model |

## Container image

A multi-stage Dockerfile produces a ~25 MB distroless image that runs as a non-root user (UID 65532), compatible with OpenShift's `restricted-v2` SCC.

```bash
docker build -t backstage-mcp-server:dev --build-arg VERSION=$(git describe --tags --always) .

# smoke-test in HTTP mode
docker run --rm -p 8080:8080 backstage-mcp-server:dev
curl -s http://localhost:8080/healthz
```

## Deploy with Helm

A Helm chart lives in `deploy/helm/backstage-mcp-server/`. It renders a Deployment, Service, optional Ingress, and — when the cluster exposes `route.openshift.io/v1` — an OpenShift Route.

### Plain Kubernetes (kind / minikube)
```bash
helm install bms deploy/helm/backstage-mcp-server \
  --set image.tag=dev \
  --set config.backstage.url=http://backstage.backstage:7007 \
  --set config.chroma.url=http://chroma.chroma:8000 \
  --set config.openai.existingSecret=openai-creds
```

### OpenShift / Red Hat Developer Hub
```bash
oc new-project mcp
helm install bms deploy/helm/backstage-mcp-server \
  -f deploy/helm/backstage-mcp-server/values-openshift.yaml \
  --set config.openai.existingSecret=openai-creds
oc get route bms-backstage-mcp-server -o jsonpath='{.spec.host}'
```

The Route terminates TLS at the edge; the pod itself listens on plain HTTP on `:8080`. Probes hit `/healthz` and `/readyz`. Secrets (Backstage token, OpenAI key) are sourced from a Kubernetes Secret you control — pass `existingSecret` rather than the inline `apiKey` for anything beyond dev.

### Verify the chart locally
```bash
helm lint deploy/helm/backstage-mcp-server
helm template demo deploy/helm/backstage-mcp-server          # plain k8s
helm template demo deploy/helm/backstage-mcp-server \
  --api-versions=route.openshift.io/v1 \
  -f deploy/helm/backstage-mcp-server/values-openshift.yaml   # OpenShift
```

## Repo layout

```
.
├── cmd/
│   ├── server/          # MCP server binary
│   └── indexer/         # one-shot document ingester
├── internal/
│   ├── backstage/       # Software Catalog REST client
│   ├── techdocs/        # TechDocs search_index.json fetcher
│   ├── rag/             # chunker, embeddings, Chroma client, Store
│   └── tools/           # MCP tool definitions and handlers
├── deploy/
│   ├── docker/          # docker-compose for ChromaDB
│   └── helm/            # Helm chart (Deployment, Service, Route, Ingress)
├── docs/sample/         # sample documents for the quickstart
├── Dockerfile           # multi-stage, distroless, non-root
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

Red Hat Developer Hub builds on Backstage and is investing in AI assistants and MCP servers for the platform. Most existing MCP servers target generic data sources (filesystem, git, databases). This project closes the gap between an LLM and a developer platform: the catalog tells the LLM *what exists*, and the TechDocs RAG layer tells it *how it works* — so an engineer can ask natural-language questions about their own infrastructure and get grounded, source-cited answers.

## License

[Apache 2.0](./LICENSE)
