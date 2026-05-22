package rag

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
)

// Document is a unit of content to be indexed. ID is optional; if empty, the
// store derives a stable hash from Source + Title + Text.
type Document struct {
	ID     string
	Source string         // e.g. "techdocs:component:default/payments" or "file:./docs/intro.md"
	Title  string         // optional human-readable title
	Text   string         // full text; will be chunked before embedding
	Extra  map[string]any // optional metadata stored alongside each chunk
}

// Hit is a query result enriched with the originating source metadata.
type Hit struct {
	Source   string         `json:"source"`
	Title    string         `json:"title,omitempty"`
	Snippet  string         `json:"snippet"`
	Distance float32        `json:"distance"`
	Extra    map[string]any `json:"extra,omitempty"`
}

// Store ties the embedder and the vector DB together and exposes a small
// surface that the MCP tools and the indexer share. It is safe for
// concurrent use by multiple goroutines.
type Store struct {
	chroma       *ChromaClient
	embedder     Embedder
	collection   string
	collectionID string
	chunkOpts    ChunkOptions
}

// StoreConfig configures the Store.
type StoreConfig struct {
	Chroma     *ChromaClient
	Embedder   Embedder
	Collection string       // collection name; default "backstage_techdocs"
	ChunkOpts  ChunkOptions // zero value falls back to DefaultChunkOptions
}

// NewStore returns a Store. The Chroma collection is resolved lazily on first
// use so construction never fails for an unreachable Chroma server (which
// would break server startup when RAG is disabled at runtime).
func NewStore(cfg StoreConfig) (*Store, error) {
	if cfg.Chroma == nil {
		return nil, fmt.Errorf("rag: Store requires a Chroma client")
	}
	if cfg.Embedder == nil {
		return nil, fmt.Errorf("rag: Store requires an Embedder")
	}
	name := cfg.Collection
	if name == "" {
		name = "backstage_techdocs"
	}
	opts := cfg.ChunkOpts
	if opts.Size == 0 {
		opts = DefaultChunkOptions()
	}
	return &Store{
		chroma:     cfg.Chroma,
		embedder:   cfg.Embedder,
		collection: name,
		chunkOpts:  opts,
	}, nil
}

// ensureCollection resolves and caches the Chroma collection ID.
func (s *Store) ensureCollection(ctx context.Context) (string, error) {
	if s.collectionID != "" {
		return s.collectionID, nil
	}
	c, err := s.chroma.EnsureCollection(ctx, s.collection)
	if err != nil {
		return "", err
	}
	s.collectionID = c.ID
	return c.ID, nil
}

// Index chunks each Document, embeds the chunks, and writes them to Chroma.
// Returns the total number of chunks written.
func (s *Store) Index(ctx context.Context, docs []Document) (int, error) {
	collID, err := s.ensureCollection(ctx)
	if err != nil {
		return 0, err
	}

	var records []Record
	for _, d := range docs {
		chunks := Chunk(d.Text, s.chunkOpts)
		if len(chunks) == 0 {
			continue
		}
		for i, chunk := range chunks {
			id := chunkID(d, i, chunk)
			meta := map[string]any{
				"source":      d.Source,
				"title":       d.Title,
				"chunk_index": i,
				"chunk_count": len(chunks),
			}
			for k, v := range d.Extra {
				meta[k] = v
			}
			records = append(records, Record{
				ID:       id,
				Document: chunk,
				Metadata: meta,
			})
		}
	}
	if len(records) == 0 {
		return 0, nil
	}

	// Embed in batches to stay well under OpenAI's per-request limits.
	const batch = 64
	for start := 0; start < len(records); start += batch {
		end := start + batch
		if end > len(records) {
			end = len(records)
		}
		inputs := make([]string, end-start)
		for i := range inputs {
			inputs[i] = records[start+i].Document
		}
		vectors, err := s.embedder.Embed(ctx, inputs)
		if err != nil {
			return 0, fmt.Errorf("rag: embed batch %d-%d: %w", start, end, err)
		}
		for i, v := range vectors {
			records[start+i].Embedding = v
		}
		if err := s.chroma.Add(ctx, collID, records[start:end]); err != nil {
			return 0, fmt.Errorf("rag: add batch %d-%d: %w", start, end, err)
		}
	}
	return len(records), nil
}

// Query embeds the query text and returns the top-k matching chunks.
func (s *Store) Query(ctx context.Context, query string, k int) ([]Hit, error) {
	collID, err := s.ensureCollection(ctx)
	if err != nil {
		return nil, err
	}
	vectors, err := s.embedder.Embed(ctx, []string{query})
	if err != nil {
		return nil, fmt.Errorf("rag: embed query: %w", err)
	}
	matches, err := s.chroma.Query(ctx, collID, vectors[0], k)
	if err != nil {
		return nil, err
	}
	hits := make([]Hit, len(matches))
	for i, m := range matches {
		h := Hit{
			Snippet:  m.Document,
			Distance: m.Distance,
		}
		if m.Metadata != nil {
			if v, ok := m.Metadata["source"].(string); ok {
				h.Source = v
			}
			if v, ok := m.Metadata["title"].(string); ok {
				h.Title = v
			}
			// Strip the standard fields and surface anything custom.
			extra := make(map[string]any, len(m.Metadata))
			for k, v := range m.Metadata {
				switch k {
				case "source", "title", "chunk_index", "chunk_count":
					continue
				default:
					extra[k] = v
				}
			}
			if len(extra) > 0 {
				h.Extra = extra
			}
		}
		hits[i] = h
	}
	return hits, nil
}

func chunkID(d Document, idx int, chunk string) string {
	if d.ID != "" {
		return fmt.Sprintf("%s#%d", d.ID, idx)
	}
	sum := sha1.Sum([]byte(d.Source + "|" + d.Title + "|" + chunk))
	return hex.EncodeToString(sum[:])
}
