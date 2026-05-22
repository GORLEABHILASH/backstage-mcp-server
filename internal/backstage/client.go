// Package backstage wraps the Backstage Software Catalog REST API.
//
// The Backstage catalog is the source of truth for components, APIs, systems,
// resources and people in a developer platform. This client exposes the
// minimal surface we need from the MCP server: search entities and fetch a
// single entity by ref.
package backstage

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client is a thin Backstage Software Catalog client.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// Config holds Backstage connection settings.
type Config struct {
	BaseURL string        // e.g. http://localhost:7007
	Token   string        // optional bearer token for protected instances
	Timeout time.Duration // request timeout; defaults to 10s when zero
}

// New constructs a Client. BaseURL is required.
func New(cfg Config) (*Client, error) {
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("backstage: BaseURL is required")
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	return &Client{
		baseURL:    strings.TrimRight(cfg.BaseURL, "/"),
		token:      cfg.Token,
		httpClient: &http.Client{Timeout: timeout},
	}, nil
}

// Entity is a partial Backstage entity (only fields we surface to the LLM).
type Entity struct {
	APIVersion string            `json:"apiVersion"`
	Kind       string            `json:"kind"`
	Metadata   EntityMetadata    `json:"metadata"`
	Spec       map[string]any    `json:"spec,omitempty"`
	Relations  []EntityRelation  `json:"relations,omitempty"`
}

type EntityMetadata struct {
	Name        string            `json:"name"`
	Namespace   string            `json:"namespace,omitempty"`
	Title       string            `json:"title,omitempty"`
	Description string            `json:"description,omitempty"`
	Tags        []string          `json:"tags,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

type EntityRelation struct {
	Type      string `json:"type"`
	TargetRef string `json:"targetRef"`
}

// SearchOptions filters the catalog query.
type SearchOptions struct {
	Kind  string // e.g. "component", "api", "system"
	Query string // free-text match against metadata.name/title/description
	Limit int    // max entities to return; defaults to 25
}

// Search returns entities matching the options. It uses Backstage's
// /api/catalog/entities endpoint with field filters where possible and does
// the free-text match client-side (Backstage server-side search lives on a
// different endpoint and is optional in many deployments).
func (c *Client) Search(ctx context.Context, opts SearchOptions) ([]Entity, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 25
	}

	q := url.Values{}
	if opts.Kind != "" {
		q.Set("filter", "kind="+opts.Kind)
	}

	endpoint := c.baseURL + "/api/catalog/entities"
	if len(q) > 0 {
		endpoint += "?" + q.Encode()
	}

	var entities []Entity
	if err := c.do(ctx, endpoint, &entities); err != nil {
		return nil, err
	}

	if opts.Query == "" {
		if len(entities) > limit {
			entities = entities[:limit]
		}
		return entities, nil
	}

	needle := strings.ToLower(opts.Query)
	matched := make([]Entity, 0, limit)
	for _, e := range entities {
		hay := strings.ToLower(e.Metadata.Name + " " + e.Metadata.Title + " " + e.Metadata.Description)
		if strings.Contains(hay, needle) {
			matched = append(matched, e)
			if len(matched) >= limit {
				break
			}
		}
	}
	return matched, nil
}

// GetByRef fetches a single entity by its Backstage entity ref
// (e.g. "component:default/payments"). Namespace defaults to "default" when
// omitted.
func (c *Client) GetByRef(ctx context.Context, ref string) (*Entity, error) {
	kind, namespace, name, err := parseEntityRef(ref)
	if err != nil {
		return nil, err
	}
	endpoint := fmt.Sprintf("%s/api/catalog/entities/by-name/%s/%s/%s",
		c.baseURL, url.PathEscape(kind), url.PathEscape(namespace), url.PathEscape(name))

	var entity Entity
	if err := c.do(ctx, endpoint, &entity); err != nil {
		return nil, err
	}
	return &entity, nil
}

func (c *Client) do(ctx context.Context, endpoint string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("backstage: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("backstage: request %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("backstage: %s returned %d: %s", endpoint, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("backstage: decode response: %w", err)
	}
	return nil
}

// parseEntityRef splits a Backstage entity ref of the form
// "<kind>:<namespace>/<name>" or "<kind>:<name>" (namespace defaults to
// "default") into its parts.
func parseEntityRef(ref string) (kind, namespace, name string, err error) {
	colon := strings.IndexByte(ref, ':')
	if colon <= 0 {
		return "", "", "", fmt.Errorf("backstage: invalid entity ref %q (expected kind:namespace/name)", ref)
	}
	kind = ref[:colon]
	rest := ref[colon+1:]

	if slash := strings.IndexByte(rest, '/'); slash >= 0 {
		namespace = rest[:slash]
		name = rest[slash+1:]
	} else {
		namespace = "default"
		name = rest
	}
	if name == "" {
		return "", "", "", fmt.Errorf("backstage: entity ref %q missing name", ref)
	}
	return kind, namespace, name, nil
}
