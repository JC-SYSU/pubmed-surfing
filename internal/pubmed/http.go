package pubmed

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

// Size limits live on two independent layers. Do not "unify" them — they
// answer different questions:
//
//   - LAYER 1, result count: how many papers one search returns to the AI
//     client (MCP response size). See DefaultRetmax / HardRetmax in query.go.
//     This is a product decision; NCBI allows far more.
//   - LAYER 2, request size: how many UIDs one HTTP request may carry
//     (transport). See esummaryGetLimit / esummaryPostLimit in service.go.
//
// A retmax of 150 is one GET (150 ≤ 200); retmax 1000 is two 500-POSTs.
// Neither number constrains the other.
const UserAgent = "PubMedSurfing/1.1.0 (local MCP tool; NCBI E-utilities)"
const DefaultTimeout = 20 * time.Second
const EutilsTimeout = 8 * time.Second

// Size limits live on two independent layers. Do not "unify" them — they
// answer different questions:
//
//   - LAYER 1, result count: how many papers one search returns to the AI
//     client (MCP response size). See DefaultRetmax / HardRetmax in query.go.
//     This is a product decision; NCBI allows far more.
//   - LAYER 2, request size: how many UIDs one HTTP request may carry
//     (transport). See EsummaryPostThreshold below (GET→POST switch, NCBI-
//     documented ~200) and esummaryPostLimit in service.go (500, NCBI's
//     undocumented JSON esummary POST hard cap, verified by experiment).
//
// A retmax of 150 is one GET (150 ≤ 200); retmax 1000 is two 500-POSTs.
// Neither number constrains the other.

type Doer interface {
	Do(*http.Request) (*http.Response, error)
}
type HTTPClient struct {
	doer  Doer
	cache *Cache
}

// EsummaryPostThreshold is the NCBI-documented GET→POST switch point for
// esummary: requests carrying more than ~200 UIDs should use HTTP POST
// (https://www.ncbi.nlm.nih.gov/books/NBK25499/). LAYER 2 of the size
// limits — transport only, unrelated to DefaultRetmax (LAYER 1, query.go).
const EsummaryPostThreshold = 200

// Post enables form-encoded POST for large multi-ID requests. Callers keep
// building url.Values; the values move into the request body while the URL
// stays bare so caching and mocks can still key off the logical parameters.
type Post struct {
	Enabled bool
}

func NewHTTPClient(doer Doer, now func() time.Time) *HTTPClient {
	if doer == nil {
		doer = http.DefaultClient
	}
	return &HTTPClient{doer: doer, cache: NewCache(now)}
}
func (c *HTTPClient) ClearCache() int { return c.cache.Clear() }
func (c *HTTPClient) Request(ctx context.Context, endpoint string, params url.Values, timeout time.Duration, cached bool) ([]byte, string, error) {
	return c.request(ctx, endpoint, params, timeout, cached, Post{})
}
func (c *HTTPClient) request(ctx context.Context, endpoint string, params url.Values, timeout time.Duration, cached bool, post Post) ([]byte, string, error) {
	encoded := params.Encode()
	key := CacheKey(endpoint, params)
	if cached {
		if b, ok := c.cache.Get(key); ok {
			return b, encoded, nil
		}
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	method := http.MethodGet
	var body io.Reader
	url := endpoint + "?" + encoded
	if post.Enabled {
		method = http.MethodPost
		body = strings.NewReader(encoded)
		url = endpoint
	}
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, encoded, err
	}
	req.Header.Set("User-Agent", UserAgent)
	if post.Enabled {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	resp, err := c.doer.Do(req)
	if err != nil {
		return nil, encoded, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, encoded, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, encoded, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(b))
	}
	if cached {
		c.cache.Set(key, b)
	}
	return b, encoded, nil
}
func (c *HTTPClient) JSON(ctx context.Context, endpoint string, p url.Values, t time.Duration) (Record, error) {
	b, _, e := c.Request(ctx, endpoint, p, t, true)
	if e != nil {
		return nil, e
	}
	var v Record
	e = json.Unmarshal(b, &v)
	return v, e
}
func (c *HTTPClient) JSONWithQuery(ctx context.Context, endpoint string, p url.Values, t time.Duration) (Record, string, error) {
	b, q, e := c.Request(ctx, endpoint, p, t, true)
	if e != nil {
		return nil, q, e
	}
	var v Record
	e = json.Unmarshal(b, &v)
	return v, q, e
}

// JSONPost behaves like JSON but sends the parameters as a form-encoded POST
// body. The cache key still comes from the logical parameters, so a POST hit
// and a GET hit share entries.
func (c *HTTPClient) JSONPost(ctx context.Context, endpoint string, p url.Values, t time.Duration) (Record, error) {
	b, _, e := c.request(ctx, endpoint, p, t, true, Post{Enabled: true})
	if e != nil {
		return nil, e
	}
	var v Record
	e = json.Unmarshal(b, &v)
	return v, e
}
func (c *HTTPClient) XML(ctx context.Context, endpoint string, p url.Values, t time.Duration, cached bool) ([]byte, error) {
	b, _, e := c.Request(ctx, endpoint, p, t, cached)
	return b, e
}
