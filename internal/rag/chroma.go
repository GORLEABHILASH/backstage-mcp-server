package rag

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Default tenant/database names used by Chroma when none are configured.
const (
	defaultTenant   = "default_tenant"
	defaultDatabase = "default_database"
)

// ChromaClient is a minimal HTTP client for the Chroma v2 REST API. It covers
// the three operations the MCP server needs: ensure a collection exists, add
// records, and query nearest neighbours.
type ChromaClient struct {
	baseURL    string
	tenant     string
	database   string
	httpClient *http.Client
}

// ChromaConfig configures the Chroma client.
type ChromaConfig struct {
	BaseURL  string        // required, e.g. http://localhost:8000
	Tenant   string        // optional; default "default_tenant"
	Database string        // optional; default "default_database"
	Timeout  time.Duration // optional; default 30s
}

// NewChromaClient constructs a Chroma client.
func NewChromaClient(cfg ChromaConfig) (*ChromaClient, error) {
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("rag: Chroma BaseURL is required")
	}
	tenant := cfg.Tenant
	if tenant == "" {
		tenant = defaultTenant
	}
	database := cfg.Database
	if database == "" {
		database = defaultDatabase
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	return &ChromaClient{
		baseURL:    strings.TrimRight(cfg.BaseURL, "/"),
		tenant:     tenant,
		database:   database,
		httpClient: &http.Client{Timeout: timeout},
	}, nil
}

// Collection is the subset of the Chroma collection response we care about.
type Collection struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type createCollectionPayload struct {
	Name        string `json:"name"`
	GetOrCreate bool   `json:"get_or_create"`
}

// EnsureCollection returns the collection with the given name, creating it if
// it does not exist (idempotent).
func (c *ChromaClient) EnsureCollection(ctx context.Context, name string) (*Collection, error) {
	endpoint := fmt.Sprintf("%s/api/v2/tenants/%s/databases/%s/collections",
		c.baseURL, c.tenant, c.database)
	body, _ := json.Marshal(createCollectionPayload{Name: name, GetOrCreate: true})
	var coll Collection
	if err := c.do(ctx, http.MethodPost, endpoint, body, &coll); err != nil {
		return nil, err
	}
	return &coll, nil
}

// Record is a single document with its embedding and metadata.
type Record struct {
	ID        string
	Embedding []float32
	Document  string
	Metadata  map[string]any
}

type addPayload struct {
	IDs        []string         `json:"ids"`
	Embeddings [][]float32      `json:"embeddings"`
	Documents  []string         `json:"documents,omitempty"`
	Metadatas  []map[string]any `json:"metadatas,omitempty"`
}

// Add inserts (or upserts on ID collision via the server's policy) records
// into the given collection.
func (c *ChromaClient) Add(ctx context.Context, collectionID string, records []Record) error {
	if len(records) == 0 {
		return nil
	}
	p := addPayload{
		IDs:        make([]string, len(records)),
		Embeddings: make([][]float32, len(records)),
		Documents:  make([]string, len(records)),
		Metadatas:  make([]map[string]any, len(records)),
	}
	for i, r := range records {
		p.IDs[i] = r.ID
		p.Embeddings[i] = r.Embedding
		p.Documents[i] = r.Document
		if r.Metadata != nil {
			p.Metadatas[i] = r.Metadata
		} else {
			p.Metadatas[i] = map[string]any{}
		}
	}
	body, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("rag: marshal add payload: %w", err)
	}
	endpoint := fmt.Sprintf("%s/api/v2/tenants/%s/databases/%s/collections/%s/add",
		c.baseURL, c.tenant, c.database, collectionID)
	return c.do(ctx, http.MethodPost, endpoint, body, nil)
}

type queryPayload struct {
	QueryEmbeddings [][]float32 `json:"query_embeddings"`
	NResults        int         `json:"n_results"`
	Include         []string    `json:"include,omitempty"`
}

// queryResponse mirrors the Chroma response shape: each field is a slice
// indexed by query, so for a single query everything is in index [0].
type queryResponse struct {
	IDs       [][]string         `json:"ids"`
	Documents [][]string         `json:"documents"`
	Metadatas [][]map[string]any `json:"metadatas"`
	Distances [][]float32        `json:"distances"`
}

// Match is one search hit.
type Match struct {
	ID       string         `json:"id"`
	Document string         `json:"document"`
	Distance float32        `json:"distance"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// Query returns the top-k nearest neighbours for a single query embedding.
func (c *ChromaClient) Query(ctx context.Context, collectionID string, embedding []float32, k int) ([]Match, error) {
	if k <= 0 {
		k = 5
	}
	body, err := json.Marshal(queryPayload{
		QueryEmbeddings: [][]float32{embedding},
		NResults:        k,
		Include:         []string{"documents", "metadatas", "distances"},
	})
	if err != nil {
		return nil, fmt.Errorf("rag: marshal query payload: %w", err)
	}
	endpoint := fmt.Sprintf("%s/api/v2/tenants/%s/databases/%s/collections/%s/query",
		c.baseURL, c.tenant, c.database, collectionID)

	var resp queryResponse
	if err := c.do(ctx, http.MethodPost, endpoint, body, &resp); err != nil {
		return nil, err
	}
	if len(resp.IDs) == 0 {
		return nil, nil
	}
	ids := resp.IDs[0]
	matches := make([]Match, len(ids))
	for i, id := range ids {
		m := Match{ID: id}
		if i < len(resp.Documents[0]) {
			m.Document = resp.Documents[0][i]
		}
		if len(resp.Distances) > 0 && i < len(resp.Distances[0]) {
			m.Distance = resp.Distances[0][i]
		}
		if len(resp.Metadatas) > 0 && i < len(resp.Metadatas[0]) {
			m.Metadata = resp.Metadatas[0][i]
		}
		matches[i] = m
	}
	return matches, nil
}

func (c *ChromaClient) do(ctx context.Context, method, endpoint string, body []byte, out any) error {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return fmt.Errorf("rag: build request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("rag: %s %s: %w", method, endpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("rag: chroma %d on %s: %s", resp.StatusCode, endpoint, strings.TrimSpace(string(b)))
	}

	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("rag: decode chroma response: %w", err)
	}
	return nil
}
