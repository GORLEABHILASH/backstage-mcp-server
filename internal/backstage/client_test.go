package backstage

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSearch_FiltersByQuery(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"apiVersion":"backstage.io/v1alpha1","kind":"Component","metadata":{"name":"payments","description":"handles payments"}},
			{"apiVersion":"backstage.io/v1alpha1","kind":"Component","metadata":{"name":"users","description":"user service"}}
		]`))
	}))
	defer srv.Close()

	c, err := New(Config{BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	got, err := c.Search(context.Background(), SearchOptions{Query: "pay"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 1 || got[0].Metadata.Name != "payments" {
		t.Fatalf("unexpected results: %+v", got)
	}
}

func TestParseEntityRef(t *testing.T) {
	cases := []struct {
		in                   string
		kind, namespace, name string
		wantErr              bool
	}{
		{"component:default/payments", "component", "default", "payments", false},
		{"component:payments", "component", "default", "payments", false},
		{"api:billing/charges-v1", "api", "billing", "charges-v1", false},
		{"missing-colon", "", "", "", true},
		{"component:", "", "", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			k, ns, n, err := parseEntityRef(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if k != tc.kind || ns != tc.namespace || n != tc.name {
				t.Fatalf("got %q/%q/%q, want %q/%q/%q", k, ns, n, tc.kind, tc.namespace, tc.name)
			}
		})
	}
}
