package rag

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Embedder turns text into vectors. The interface exists so we can swap in a
// fake embedder for tests without touching the network.
type Embedder interface {
	Embed(ctx context.Context, inputs []string) ([][]float32, error)
	Model() string
	Dimensions() int
}

// OpenAIEmbedder calls the OpenAI /v1/embeddings endpoint.
//
// Defaults to text-embedding-3-small (1536 dims, 8192-token context, currently
// the cheapest embedding model with strong quality). The model is exposed as
// a config option so users can switch without recompiling.
type OpenAIEmbedder struct {
	apiKey     string
	model      string
	dims       int
	endpoint   string
	httpClient *http.Client
}

// OpenAIConfig configures the embedder.
type OpenAIConfig struct {
	APIKey     string        // required
	Model      string        // optional; default text-embedding-3-small
	BaseURL    string        // optional; default https://api.openai.com/v1
	Timeout    time.Duration // optional; default 30s
	Dimensions int           // optional; only used to report .Dimensions(); 1536 for default model
}

// NewOpenAIEmbedder returns a configured embedder.
func NewOpenAIEmbedder(cfg OpenAIConfig) (*OpenAIEmbedder, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("rag: OpenAI API key is required")
	}
	model := cfg.Model
	if model == "" {
		model = "text-embedding-3-small"
	}
	base := cfg.BaseURL
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	dims := cfg.Dimensions
	if dims == 0 {
		dims = defaultDimsFor(model)
	}
	return &OpenAIEmbedder{
		apiKey:     cfg.APIKey,
		model:      model,
		dims:       dims,
		endpoint:   base + "/embeddings",
		httpClient: &http.Client{Timeout: timeout},
	}, nil
}

func (e *OpenAIEmbedder) Model() string   { return e.model }
func (e *OpenAIEmbedder) Dimensions() int { return e.dims }

type embedRequest struct {
	Input []string `json:"input"`
	Model string   `json:"model"`
}

type embedResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
}

// Embed returns one vector per input string, in order.
func (e *OpenAIEmbedder) Embed(ctx context.Context, inputs []string) ([][]float32, error) {
	if len(inputs) == 0 {
		return nil, nil
	}
	body, err := json.Marshal(embedRequest{Input: inputs, Model: e.model})
	if err != nil {
		return nil, fmt.Errorf("rag: marshal embed request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("rag: build embed request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+e.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("rag: embed request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("rag: embeddings %d: %s", resp.StatusCode, string(b))
	}

	var out embedResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("rag: decode embeddings: %w", err)
	}
	if len(out.Data) != len(inputs) {
		return nil, fmt.Errorf("rag: expected %d embeddings, got %d", len(inputs), len(out.Data))
	}
	vectors := make([][]float32, len(inputs))
	for _, d := range out.Data {
		if d.Index < 0 || d.Index >= len(vectors) {
			return nil, fmt.Errorf("rag: out-of-range embedding index %d", d.Index)
		}
		vectors[d.Index] = d.Embedding
	}
	return vectors, nil
}

func defaultDimsFor(model string) int {
	switch model {
	case "text-embedding-3-small":
		return 1536
	case "text-embedding-3-large":
		return 3072
	case "text-embedding-ada-002":
		return 1536
	default:
		return 0 // caller can override via OpenAIConfig.Dimensions
	}
}
