BINARY := backstage-mcp-server
PKG    := ./cmd/server

.PHONY: build test run tidy fmt vet clean inspect

build:
	go build -o bin/$(BINARY) $(PKG)

test:
	go test ./...

run: build
	./bin/$(BINARY)

tidy:
	go mod tidy

fmt:
	gofmt -s -w .

vet:
	go vet ./...

clean:
	rm -rf bin/

## Launch the official MCP Inspector against the local server (requires Node.js).
inspect: build
	npx @modelcontextprotocol/inspector ./bin/$(BINARY)
