SERVER  := backstage-mcp-server
INDEXER := backstage-mcp-indexer

.PHONY: build build-server build-indexer test run run-indexer tidy fmt vet clean inspect chroma-up chroma-down index-sample

build: build-server build-indexer

build-server:
	go build -o bin/$(SERVER) ./cmd/server

build-indexer:
	go build -o bin/$(INDEXER) ./cmd/indexer

test:
	go test ./...

run: build-server
	./bin/$(SERVER)

run-indexer: build-indexer
	./bin/$(INDEXER)

tidy:
	go mod tidy

fmt:
	gofmt -s -w .

vet:
	go vet ./...

clean:
	rm -rf bin/

## Launch the official MCP Inspector against the local server (requires Node.js).
inspect: build-server
	npx @modelcontextprotocol/inspector ./bin/$(SERVER)

## Start ChromaDB locally (port 8000).
chroma-up:
	docker compose -f deploy/docker/docker-compose.yml up -d

chroma-down:
	docker compose -f deploy/docker/docker-compose.yml down

## Index the bundled sample docs (requires OPENAI_API_KEY and a running Chroma).
index-sample: build-indexer
	./bin/$(INDEXER) --source=files --path=./docs/sample
