// Command indexer ingests documents into the RAG store powering the
// query_docs MCP tool. Two sources are supported:
//
//	files      — read .md / .txt files from a directory tree
//	techdocs   — pull a Backstage TechDocs search index for one or more
//	             entity refs
//
// Examples:
//
//	indexer --source=files --path=./docs
//	indexer --source=techdocs --backstage-url=http://localhost:7007 \
//	        --refs=component:default/payments,component:default/users
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/GORLEABHILASH/backstage-mcp-server/internal/rag"
	"github.com/GORLEABHILASH/backstage-mcp-server/internal/techdocs"
)

func main() {
	source := flag.String("source", "files", "Document source: files | techdocs")

	// Files mode
	path := flag.String("path", "./docs", "Root directory to walk (files mode)")
	exts := flag.String("exts", ".md,.markdown,.txt", "Comma-separated list of file extensions to include (files mode)")

	// TechDocs mode
	backstageURL := flag.String("backstage-url", os.Getenv("BACKSTAGE_URL"), "Backstage base URL (techdocs mode)")
	backstageToken := flag.String("backstage-token", os.Getenv("BACKSTAGE_TOKEN"), "Backstage bearer token (techdocs mode)")
	refs := flag.String("refs", "", "Comma-separated entity refs to index (techdocs mode, e.g. component:default/payments)")

	// RAG store config
	chromaURL := flag.String("chroma-url", envOr("CHROMA_URL", "http://localhost:8000"), "ChromaDB base URL")
	chromaCollection := flag.String("chroma-collection", envOr("CHROMA_COLLECTION", "backstage_techdocs"), "Chroma collection name")
	openaiKey := flag.String("openai-key", os.Getenv("OPENAI_API_KEY"), "OpenAI API key for embeddings")
	embedModel := flag.String("embed-model", envOr("EMBED_MODEL", "text-embedding-3-small"), "OpenAI embedding model")

	flag.Parse()

	if *openaiKey == "" {
		log.Fatal("OPENAI_API_KEY is required (or pass --openai-key)")
	}

	store, err := buildStore(*chromaURL, *chromaCollection, *openaiKey, *embedModel)
	if err != nil {
		log.Fatalf("init RAG store: %v", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	var docs []rag.Document
	switch *source {
	case "files":
		docs, err = loadFiles(*path, splitCSV(*exts))
	case "techdocs":
		if *backstageURL == "" || *refs == "" {
			log.Fatal("techdocs mode requires --backstage-url and --refs")
		}
		docs, err = loadTechDocs(ctx, *backstageURL, *backstageToken, splitCSV(*refs))
	default:
		log.Fatalf("unknown --source %q (want files|techdocs)", *source)
	}
	if err != nil {
		log.Fatalf("load documents: %v", err)
	}
	if len(docs) == 0 {
		log.Println("no documents found; nothing to index")
		return
	}

	log.Printf("indexing %d documents into %s (collection=%s) ...", len(docs), *chromaURL, *chromaCollection)
	chunks, err := store.Index(ctx, docs)
	if err != nil {
		log.Fatalf("index: %v", err)
	}
	log.Printf("done — wrote %d chunks", chunks)
}

func buildStore(chromaURL, collection, openaiKey, model string) (*rag.Store, error) {
	chroma, err := rag.NewChromaClient(rag.ChromaConfig{BaseURL: chromaURL})
	if err != nil {
		return nil, err
	}
	embedder, err := rag.NewOpenAIEmbedder(rag.OpenAIConfig{APIKey: openaiKey, Model: model})
	if err != nil {
		return nil, err
	}
	return rag.NewStore(rag.StoreConfig{
		Chroma:     chroma,
		Embedder:   embedder,
		Collection: collection,
	})
}

func loadFiles(root string, allowedExts []string) ([]rag.Document, error) {
	root = filepath.Clean(root)
	allowed := make(map[string]bool, len(allowedExts))
	for _, e := range allowedExts {
		allowed[strings.ToLower(strings.TrimSpace(e))] = true
	}

	var docs []rag.Document
	err := filepath.Walk(root, func(p string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		if !allowed[strings.ToLower(filepath.Ext(p))] {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return fmt.Errorf("read %s: %w", p, err)
		}
		rel, _ := filepath.Rel(root, p)
		docs = append(docs, rag.Document{
			Source: "file:" + rel,
			Title:  filepath.Base(p),
			Text:   string(data),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return docs, nil
}

func loadTechDocs(ctx context.Context, backstageURL, token string, refs []string) ([]rag.Document, error) {
	client, err := techdocs.New(techdocs.Config{BaseURL: backstageURL, Token: token})
	if err != nil {
		return nil, err
	}

	var docs []rag.Document
	for _, ref := range refs {
		kind, ns, name, err := parseRef(ref)
		if err != nil {
			log.Printf("skip %q: %v", ref, err)
			continue
		}
		pages, err := client.FetchIndex(ctx, ns, kind, name)
		if err != nil {
			log.Printf("fetch %s: %v", ref, err)
			continue
		}
		log.Printf("%s: %d pages", ref, len(pages))
		for _, page := range pages {
			docs = append(docs, rag.Document{
				Source: page.EntityRef,
				Title:  page.Title,
				Text:   page.Text,
				Extra: map[string]any{
					"location":   page.Location,
					"entity_ref": page.EntityRef,
				},
			})
		}
	}
	return docs, nil
}

func parseRef(ref string) (kind, namespace, name string, err error) {
	colon := strings.IndexByte(ref, ':')
	if colon <= 0 {
		return "", "", "", fmt.Errorf("invalid ref %q (expected kind:namespace/name)", ref)
	}
	kind = ref[:colon]
	rest := ref[colon+1:]
	if slash := strings.IndexByte(rest, '/'); slash >= 0 {
		namespace, name = rest[:slash], rest[slash+1:]
	} else {
		namespace, name = "default", rest
	}
	if name == "" {
		return "", "", "", fmt.Errorf("ref %q missing name", ref)
	}
	return kind, namespace, name, nil
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := parts[:0]
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
