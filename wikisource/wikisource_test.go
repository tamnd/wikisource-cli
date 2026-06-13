package wikisource_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tamnd/wikisource-cli/wikisource"
)

func newTestClient(baseURL string) *wikisource.Client {
	cfg := wikisource.DefaultConfig()
	cfg.BaseURL = baseURL
	cfg.Rate = 0
	return wikisource.NewClient(cfg)
}

func TestGetSendsUserAgent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Error("request carried no User-Agent")
		}
		// return a valid search response
		resp := map[string]any{
			"query": map[string]any{
				"search": []any{},
			},
		}
		b, _ := json.Marshal(resp)
		_, _ = w.Write(b)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, err := c.Search(context.Background(), "test", 1)
	if err != nil {
		t.Fatal(err)
	}
}

func TestGetRetriesOn503(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		resp := map[string]any{
			"query": map[string]any{
				"search": []any{},
			},
		}
		b, _ := json.Marshal(resp)
		_, _ = w.Write(b)
	}))
	defer srv.Close()

	cfg := wikisource.DefaultConfig()
	cfg.BaseURL = srv.URL
	cfg.Rate = 0
	cfg.Retries = 5
	c := wikisource.NewClient(cfg)

	start := time.Now()
	_, err := c.Search(context.Background(), "test", 1)
	if err != nil {
		t.Fatal(err)
	}
	if hits != 3 {
		t.Errorf("server saw %d hits, want 3", hits)
	}
	if time.Since(start) < 500*time.Millisecond {
		t.Error("retries did not back off")
	}
}

func TestSearch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"query": map[string]any{
				"search": []any{
					map[string]any{"title": "The Raven", "snippet": "Once upon a midnight dreary"},
					map[string]any{"title": "Hamlet", "snippet": "To be or not to be"},
				},
			},
		}
		b, _ := json.Marshal(resp)
		_, _ = w.Write(b)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	results, err := c.Search(context.Background(), "Shakespeare", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	if results[0].Title != "The Raven" {
		t.Errorf("got title %q, want %q", results[0].Title, "The Raven")
	}
	if results[0].URL == "" {
		t.Error("URL should not be empty")
	}
}

func TestRandom(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"query": map[string]any{
				"random": []any{
					map[string]any{"title": "Paradise Lost"},
					map[string]any{"title": "Ulysses"},
				},
			},
		}
		b, _ := json.Marshal(resp)
		_, _ = w.Write(b)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	results, err := c.Random(context.Background(), 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	if results[0].Title != "Paradise Lost" {
		t.Errorf("got %q, want %q", results[0].Title, "Paradise Lost")
	}
}

func TestList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"query": map[string]any{
				"categorymembers": []any{
					map[string]any{"title": "Frankenstein"},
					map[string]any{"title": "Dracula"},
				},
			},
		}
		b, _ := json.Marshal(resp)
		_, _ = w.Write(b)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	results, err := c.List(context.Background(), "Novels", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	if results[0].Title != "Frankenstein" {
		t.Errorf("got %q, want %q", results[0].Title, "Frankenstein")
	}
}

func TestSuggest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// opensearch format: [query, [titles], [descriptions], [urls]]
		resp := []any{
			"hamlet",
			[]string{"Hamlet", "Hamlet (novel)"},
			[]string{"", ""},
			[]string{"https://en.wikisource.org/wiki/Hamlet", "https://en.wikisource.org/wiki/Hamlet_(novel)"},
		}
		b, _ := json.Marshal(resp)
		_, _ = w.Write(b)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	results, err := c.Suggest(context.Background(), "hamlet", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	if results[0].Title != "Hamlet" {
		t.Errorf("got %q, want %q", results[0].Title, "Hamlet")
	}
	if results[0].URL != "https://en.wikisource.org/wiki/Hamlet" {
		t.Errorf("got URL %q", results[0].URL)
	}
}

func TestTextNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"query": map[string]any{
				"pages": map[string]any{
					"-1": map[string]any{
						"title":   "Missing Page",
						"missing": "",
					},
				},
			},
		}
		b, _ := json.Marshal(resp)
		_, _ = w.Write(b)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, err := c.Text(context.Background(), "Missing Page")
	if err != wikisource.ErrNotFound {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
}

func TestTextFetch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"query": map[string]any{
				"pages": map[string]any{
					"12345": map[string]any{
						"title": "The Raven",
						"revisions": []any{
							map[string]any{"*": "{{header|The Raven}}\nOnce upon a midnight dreary, while I pondered, weak and weary,\n[[wikilink|Over]] many a quaint and curious volume of forgotten lore."},
						},
					},
				},
			},
		}
		b, _ := json.Marshal(resp)
		_, _ = w.Write(b)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	text, err := c.Text(context.Background(), "The Raven")
	if err != nil {
		t.Fatal(err)
	}
	if text == "" {
		t.Error("text should not be empty")
	}
	// template should be stripped
	if strings.Contains(text, "{{header") {
		t.Error("template not stripped from text")
	}
	// wikilink should be resolved to display text
	if strings.Contains(text, "[[") {
		t.Error("wikilinks not stripped from text")
	}
	if !strings.Contains(text, "midnight dreary") {
		t.Error("content text missing after cleanup")
	}
}

func TestStripWikiMarkup(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{`{{header|The Raven|author=Poe}}`, ""},
		{`[[link|display text]]`, "display text"},
		{`[[simple link]]`, "simple link"},
		{`==Section==`, "Section"},
		{`<ref>footnote</ref>text`, "text"},
		{`'''bold'''`, "bold"},
		{`''italic''`, "italic"},
	}
	for _, tc := range cases {
		got := wikisource.StripWikiMarkup(tc.in)
		if got != tc.want {
			t.Errorf("StripWikiMarkup(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
