package pubmed

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestFetchSummariesUsesPostAboveThreshold(t *testing.T) {
	var methods []string
	var postBatches []int
	d := doerFunc(func(r *http.Request) (*http.Response, error) {
		methods = append(methods, r.Method)
		if r.Method == http.MethodPost {
			b, _ := io.ReadAll(r.Body)
			if ct := r.Header.Get("Content-Type"); ct != "application/x-www-form-urlencoded" {
				t.Fatalf("content type %q", ct)
			}
			v, e := url.ParseQuery(string(b))
			if e != nil {
				t.Fatal(e)
			}
			postBatches = append(postBatches, len(strings.Split(v.Get("id"), ",")))
			if r.URL.RawQuery != "" {
				t.Fatalf("POST should carry no query, got %s", r.URL.RawQuery)
			}
		}
		return response(`{"result":{}}`), nil
	})
	s := NewService(Options{HTTP: NewHTTPClient(d, nil), Sleep: func(context.Context, time.Duration) error { return nil }})
	ids := make([]string, 1200)
	for i := range ids {
		ids[i] = strconv.Itoa(i)
	}
	if _, e := s.fetchSummaries(context.Background(), ids); e != nil {
		t.Fatal(e)
	}
	// 201..1200 IDs: no GET at all, POST batches capped at 500.
	if len(methods) != 3 || methods[0] != "POST" {
		t.Fatalf("methods: %v", methods)
	}
	if postBatches[0] != 500 || postBatches[1] != 500 || postBatches[2] != 200 {
		t.Fatalf("post batches: %v", postBatches)
	}
}

func TestFetchSummariesSmallListStaysGet(t *testing.T) {
	var methods []string
	d := doerFunc(func(r *http.Request) (*http.Response, error) {
		methods = append(methods, r.Method)
		return response(`{"result":{"1":{"title":"T"}}}`), nil
	})
	s := NewService(Options{HTTP: NewHTTPClient(d, nil), Sleep: func(context.Context, time.Duration) error { return nil }})
	r, e := s.fetchSummaries(context.Background(), []string{"1"})
	if e != nil {
		t.Fatal(e)
	}
	if len(methods) != 1 || methods[0] != "GET" {
		t.Fatalf("methods: %v", methods)
	}
	if asMap(r["result"])["1"] == nil {
		t.Fatal(r)
	}
}
