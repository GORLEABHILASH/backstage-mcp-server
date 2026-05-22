// Package techdocs pulls documentation content out of a Backstage instance.
//
// Backstage TechDocs are built from MkDocs and every build emits a flat
// search index at
//
//	/api/techdocs/static/docs/{namespace}/{kind}/{name}/search/search_index.json
//
// That file contains pre-segmented {location, title, text} records covering
// every page. Using it lets us ingest documentation without parsing HTML.
package techdocs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client fetches TechDocs search indexes from Backstage.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// Config configures the TechDocs client.
type Config struct {
	BaseURL string        // Backstage base URL, e.g. http://localhost:7007
	Token   string        // optional bearer token
	Timeout time.Duration // optional; default 15s
}

// New constructs a TechDocs client.
func New(cfg Config) (*Client, error) {
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("techdocs: BaseURL is required")
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 15 * time.Second
	}
	return &Client{
		baseURL:    strings.TrimRight(cfg.BaseURL, "/"),
		token:      cfg.Token,
		httpClient: &http.Client{Timeout: timeout},
	}, nil
}

// Doc is a single TechDocs page extracted from the MkDocs search index.
type Doc struct {
	EntityRef string // e.g. "component:default/payments"
	Location  string // relative URL within the docs site, e.g. "intro/#getting-started"
	Title     string
	Text      string
}

// mkdocsIndex matches the on-disk shape of the file MkDocs writes.
type mkdocsIndex struct {
	Docs []struct {
		Location string `json:"location"`
		Title    string `json:"title"`
		Text     string `json:"text"`
	} `json:"docs"`
}

// FetchIndex pulls the search index for one entity and returns one Doc per
// page. namespace defaults to "default" when empty; kind defaults to
// "component".
func (c *Client) FetchIndex(ctx context.Context, namespace, kind, name string) ([]Doc, error) {
	if namespace == "" {
		namespace = "default"
	}
	if kind == "" {
		kind = "component"
	}
	if name == "" {
		return nil, fmt.Errorf("techdocs: name is required")
	}

	url := fmt.Sprintf("%s/api/techdocs/static/docs/%s/%s/%s/search/search_index.json",
		c.baseURL, namespace, kind, name)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("techdocs: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("techdocs: fetch %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil // entity has no published TechDocs
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("techdocs: %s returned %d: %s", url, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var idx mkdocsIndex
	if err := json.NewDecoder(resp.Body).Decode(&idx); err != nil {
		return nil, fmt.Errorf("techdocs: decode index: %w", err)
	}

	entityRef := fmt.Sprintf("%s:%s/%s", strings.ToLower(kind), namespace, name)
	out := make([]Doc, 0, len(idx.Docs))
	for _, d := range idx.Docs {
		text := strings.TrimSpace(d.Text)
		if text == "" {
			continue
		}
		out = append(out, Doc{
			EntityRef: entityRef,
			Location:  d.Location,
			Title:     d.Title,
			Text:      text,
		})
	}
	return out, nil
}
