// Package wikisource is the library behind the wsrc command line:
// the HTTP client, request shaping, and the typed data models for English Wikisource.
//
// The MediaWiki API at https://en.wikisource.org/w/api.php is open and requires
// no authentication key. The Client sets a polite User-Agent, paces requests,
// and retries transient failures (429 and 5xx).
package wikisource

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const defaultBase = "https://en.wikisource.org"

// DefaultUserAgent identifies the client to Wikisource.
const DefaultUserAgent = "wsrc/dev (+https://github.com/tamnd/wikisource-cli)"

// ErrNotFound is returned when the API returns no pages for a title.
var ErrNotFound = errors.New("not found")

// Config holds constructor parameters.
type Config struct {
	BaseURL   string
	UserAgent string
	Rate      time.Duration
	Retries   int
	Timeout   time.Duration
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		BaseURL:   defaultBase,
		UserAgent: DefaultUserAgent,
		Rate:      200 * time.Millisecond,
		Retries:   5,
		Timeout:   30 * time.Second,
	}
}

// Client talks to the English Wikisource MediaWiki API.
type Client struct {
	http      *http.Client
	userAgent string
	baseURL   string
	rate      time.Duration
	retries   int
	last      time.Time
}

// NewClient returns a Client configured from cfg.
func NewClient(cfg Config) *Client {
	base := cfg.BaseURL
	if base == "" {
		base = defaultBase
	}
	return &Client{
		http:      &http.Client{Timeout: cfg.Timeout},
		userAgent: cfg.UserAgent,
		baseURL:   base,
		rate:      cfg.Rate,
		retries:   cfg.Retries,
	}
}

func (c *Client) apiURL() string {
	return c.baseURL + "/w/api.php"
}

func (c *Client) pageURL(title string) string {
	return c.baseURL + "/wiki/" + url.PathEscape(strings.ReplaceAll(title, " ", "_"))
}

// get fetches a URL with pacing and retries.
func (c *Client) get(ctx context.Context, rawURL string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= c.retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff(attempt)):
			}
		}
		body, retry, err := c.do(ctx, rawURL)
		if err == nil {
			return body, nil
		}
		lastErr = err
		if !retry {
			return nil, err
		}
	}
	return nil, fmt.Errorf("get %s: %w", rawURL, lastErr)
}

func (c *Client) do(ctx context.Context, rawURL string) ([]byte, bool, error) {
	c.pace()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, true, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return nil, true, fmt.Errorf("http %d", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("http %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, true, err
	}
	return b, false, nil
}

func (c *Client) pace() {
	if c.rate <= 0 {
		return
	}
	if wait := c.rate - time.Since(c.last); wait > 0 {
		time.Sleep(wait)
	}
	c.last = time.Now()
}

func backoff(attempt int) time.Duration {
	d := time.Duration(attempt) * 500 * time.Millisecond
	if d > 5*time.Second {
		d = 5 * time.Second
	}
	return d
}

func (c *Client) getJSON(ctx context.Context, rawURL string, v any) error {
	body, err := c.get(ctx, rawURL)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("decode %s: %w", rawURL, err)
	}
	return nil
}

// ─── Types ───────────────────────────────────────────────────────────────────

// Text is a Wikisource text or document record.
type Text struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

// SearchResult is the record for a search hit.
type SearchResult struct {
	Title   string `json:"title"`
	Snippet string `json:"snippet"`
	URL     string `json:"url"`
}

// ─── API wire types ──────────────────────────────────────────────────────────

type searchResp struct {
	Query struct {
		Search []struct {
			Title   string `json:"title"`
			Snippet string `json:"snippet"`
		} `json:"search"`
	} `json:"query"`
}

type randomResp struct {
	Query struct {
		Random []struct {
			Title string `json:"title"`
		} `json:"random"`
	} `json:"query"`
}

type categoryResp struct {
	Query struct {
		CategoryMembers []struct {
			Title string `json:"title"`
		} `json:"categorymembers"`
	} `json:"query"`
}

type revisionResp struct {
	Query struct {
		Pages map[string]struct {
			Title     string `json:"title"`
			Missing   string `json:"missing"`
			Revisions []struct {
				Content string `json:"*"`
			} `json:"revisions"`
		} `json:"pages"`
	} `json:"query"`
}

type openSearchResp [4]json.RawMessage

// ─── Methods ─────────────────────────────────────────────────────────────────

// Search searches Wikisource texts using the MediaWiki search API.
func (c *Client) Search(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	if limit <= 0 {
		limit = 10
	}
	params := url.Values{}
	params.Set("action", "query")
	params.Set("list", "search")
	params.Set("srsearch", query)
	params.Set("srlimit", fmt.Sprintf("%d", limit))
	params.Set("format", "json")
	params.Set("srprop", "snippet")

	rawURL := c.apiURL() + "?" + params.Encode()
	var resp searchResp
	if err := c.getJSON(ctx, rawURL, &resp); err != nil {
		return nil, err
	}

	out := make([]SearchResult, 0, len(resp.Query.Search))
	for _, s := range resp.Query.Search {
		out = append(out, SearchResult{
			Title:   s.Title,
			Snippet: stripHTMLTags(s.Snippet),
			URL:     c.pageURL(s.Title),
		})
	}
	return out, nil
}

// Text fetches the wikitext content of a page and returns cleaned readable text.
func (c *Client) Text(ctx context.Context, title string) (string, error) {
	params := url.Values{}
	params.Set("action", "query")
	params.Set("prop", "revisions")
	params.Set("rvprop", "content")
	params.Set("titles", title)
	params.Set("format", "json")

	rawURL := c.apiURL() + "?" + params.Encode()
	var resp revisionResp
	if err := c.getJSON(ctx, rawURL, &resp); err != nil {
		return "", err
	}

	for _, page := range resp.Query.Pages {
		if page.Missing == "" && len(page.Revisions) > 0 {
			return StripWikiMarkup(page.Revisions[0].Content), nil
		}
	}
	return "", ErrNotFound
}

// Random returns a list of random Wikisource pages.
func (c *Client) Random(ctx context.Context, limit int) ([]Text, error) {
	if limit <= 0 {
		limit = 5
	}
	params := url.Values{}
	params.Set("action", "query")
	params.Set("list", "random")
	params.Set("rnnamespace", "0")
	params.Set("rnlimit", fmt.Sprintf("%d", limit))
	params.Set("format", "json")

	rawURL := c.apiURL() + "?" + params.Encode()
	var resp randomResp
	if err := c.getJSON(ctx, rawURL, &resp); err != nil {
		return nil, err
	}

	out := make([]Text, 0, len(resp.Query.Random))
	for _, r := range resp.Query.Random {
		out = append(out, Text{
			Title: r.Title,
			URL:   c.pageURL(r.Title),
		})
	}
	return out, nil
}

// List lists pages in a category. If category is empty, falls back to search.
func (c *Client) List(ctx context.Context, category string, limit int) ([]Text, error) {
	if limit <= 0 {
		limit = 20
	}
	if category == "" {
		// fall back to search with a broad query
		results, err := c.Search(ctx, "", limit)
		if err != nil {
			return nil, err
		}
		out := make([]Text, len(results))
		for i, r := range results {
			out[i] = Text{Title: r.Title, URL: r.URL, Snippet: r.Snippet}
		}
		return out, nil
	}

	cat := category
	if !strings.HasPrefix(strings.ToLower(cat), "category:") {
		cat = "Category:" + cat
	}

	params := url.Values{}
	params.Set("action", "query")
	params.Set("list", "categorymembers")
	params.Set("cmtitle", cat)
	params.Set("cmlimit", fmt.Sprintf("%d", limit))
	params.Set("cmnamespace", "0")
	params.Set("format", "json")

	rawURL := c.apiURL() + "?" + params.Encode()
	var resp categoryResp
	if err := c.getJSON(ctx, rawURL, &resp); err != nil {
		return nil, err
	}

	out := make([]Text, 0, len(resp.Query.CategoryMembers))
	for _, m := range resp.Query.CategoryMembers {
		out = append(out, Text{
			Title: m.Title,
			URL:   c.pageURL(m.Title),
		})
	}
	return out, nil
}

// Suggest returns title suggestions using the OpenSearch API.
func (c *Client) Suggest(ctx context.Context, prefix string, limit int) ([]Text, error) {
	if limit <= 0 {
		limit = 10
	}
	params := url.Values{}
	params.Set("action", "opensearch")
	params.Set("search", prefix)
	params.Set("limit", fmt.Sprintf("%d", limit))
	params.Set("namespace", "0")
	params.Set("format", "json")

	rawURL := c.apiURL() + "?" + params.Encode()
	body, err := c.get(ctx, rawURL)
	if err != nil {
		return nil, err
	}

	// OpenSearch returns [query, [titles], [descriptions], [urls]]
	var raw openSearchResp
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode opensearch: %w", err)
	}

	var titles, descriptions, urls []string
	if err := json.Unmarshal(raw[1], &titles); err != nil {
		return nil, fmt.Errorf("decode titles: %w", err)
	}
	// descriptions may be empty strings
	_ = json.Unmarshal(raw[2], &descriptions)
	_ = json.Unmarshal(raw[3], &urls)

	out := make([]Text, 0, len(titles))
	for i, t := range titles {
		snippet := ""
		if i < len(descriptions) {
			snippet = descriptions[i]
		}
		u := c.pageURL(t)
		if i < len(urls) && urls[i] != "" {
			u = urls[i]
		}
		out = append(out, Text{
			Title:   t,
			URL:     u,
			Snippet: snippet,
		})
	}
	return out, nil
}

// ─── Wikitext cleanup ────────────────────────────────────────────────────────

var (
	// {{template|...}} — greedy inside braces
	reTpl = regexp.MustCompile(`\{\{[^{}]*(?:\{\{[^{}]*\}\}[^{}]*)?\}\}`)
	// [[File:...]] or [[Category:...]] with possible caption
	reFileLink = regexp.MustCompile(`\[\[(?:File|Category|Image|Media):[^\]]*\]\]`)
	// [[link|text]] → text, or [[link]] → link
	reWikiLink = regexp.MustCompile(`\[\[(?:[^\]|]*\|)?([^\]]*)\]\]`)
	// ==Section== headings
	reHeading = regexp.MustCompile(`(?m)^={1,6}([^=]+)={1,6}\s*$`)
	// HTML tags
	reHTMLTag = regexp.MustCompile(`<[^>]+>`)
	// <ref>...</ref>
	reRef = regexp.MustCompile(`(?s)<ref[^>]*>.*?</ref>`)
	// '''bold''' and ''italic''
	reFormatting = regexp.MustCompile(`'{2,3}`)
	// multiple blank lines
	reBlankLines = regexp.MustCompile(`\n{3,}`)
)

// StripWikiMarkup removes wiki markup from wikitext for readable output.
func StripWikiMarkup(s string) string {
	// remove <ref>...</ref> blocks first
	s = reRef.ReplaceAllString(s, "")
	// remove self-closing refs
	s = regexp.MustCompile(`<ref[^/]*/>`).ReplaceAllString(s, "")
	// remove templates (multiple passes for nesting)
	for i := 0; i < 5; i++ {
		s2 := reTpl.ReplaceAllString(s, "")
		if s2 == s {
			break
		}
		s = s2
	}
	// remove file/category links
	s = reFileLink.ReplaceAllString(s, "")
	// convert wikilinks to display text: [[link|text]] → text, [[link]] → link
	s = reWikiLink.ReplaceAllStringFunc(s, func(m string) string {
		inner := m[2 : len(m)-2] // strip [[ and ]]
		if idx := strings.Index(inner, "|"); idx >= 0 {
			return inner[idx+1:]
		}
		return inner
	})
	// convert headings to plain text
	s = reHeading.ReplaceAllString(s, "\n$1\n")
	// remove HTML tags
	s = reHTMLTag.ReplaceAllString(s, "")
	// remove bold/italic markers
	s = reFormatting.ReplaceAllString(s, "")
	// collapse excessive blank lines
	s = reBlankLines.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}

// stripHTMLTags removes HTML tags from a string (for API snippets).
func stripHTMLTags(s string) string {
	s = reHTMLTag.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	s = strings.ReplaceAll(s, "&quot;", `"`)
	s = strings.ReplaceAll(s, "&#39;", "'")
	return strings.TrimSpace(s)
}
