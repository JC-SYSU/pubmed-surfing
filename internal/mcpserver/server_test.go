package mcpserver

import (
	"context"
	"sort"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestRegistrySchemasAndClearCache(t *testing.T) {
	ctx := context.Background()
	ct, st := mcp.NewInMemoryTransports()
	server := New(nil)
	ss, e := server.Connect(ctx, st, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer ss.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	cs, e := client.Connect(ctx, ct, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer cs.Close()
	listed, e := cs.ListTools(ctx, nil)
	if e != nil {
		t.Fatal(e)
	}
	names := []string{}
	by := map[string]*mcp.Tool{}
	for _, tool := range listed.Tools {
		names = append(names, tool.Name)
		by[tool.Name] = tool
	}
	sort.Strings(names)
	want := []string{"pubmed_clear_cache", "pubmed_convert_ids", "pubmed_fetch_abstract", "pubmed_fetch_abstracts_batch", "pubmed_find_related", "pubmed_format_citations", "pubmed_get_total_count", "pubmed_journal_mesh_profile", "pubmed_journal_profile", "pubmed_search", "pubmed_verify_article_type"}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("tools: %v", names)
		}
	}
	search := by["pubmed_search"].InputSchema.(map[string]any)
	props := search["properties"].(map[string]any)
	if props["retmax"].(map[string]any)["default"] != float64(10) && props["retmax"].(map[string]any)["default"] != 10 {
		t.Fatal("retmax default")
	}
	batch := by["pubmed_fetch_abstracts_batch"].InputSchema.(map[string]any)
	if batch["required"].([]any)[0] != "pmids" {
		t.Fatalf("required: %#v", batch["required"])
	}
	citations := by["pubmed_format_citations"].InputSchema.(map[string]any)
	papers := citations["properties"].(map[string]any)["papers"].(map[string]any)
	if _, hasAnyOf := papers["anyOf"]; hasAnyOf {
		t.Fatal("papers must use a type array for OpenCode structured-argument compatibility")
	}
	if got := papers["type"].([]any); len(got) != 2 || got[0] != "array" || got[1] != "null" {
		t.Fatalf("papers type: %#v", papers["type"])
	}
	res, e := cs.CallTool(ctx, &mcp.CallToolParams{Name: "pubmed_clear_cache", Arguments: map[string]any{}})
	if e != nil || res.IsError {
		t.Fatal(e, res)
	}
	if res.StructuredContent == nil {
		t.Fatal("structured content missing")
	}
	invalid, invalidErr := cs.CallTool(ctx, &mcp.CallToolParams{Name: "pubmed_fetch_abstract", Arguments: map[string]any{}})
	if invalidErr == nil && (invalid == nil || !invalid.IsError) {
		t.Fatalf("missing required pmid was accepted: %#v", invalid)
	}
	searchResult, e := cs.CallTool(ctx, &mcp.CallToolParams{Name: "pubmed_search", Arguments: map[string]any{}})
	if e != nil || searchResult.IsError {
		t.Fatal("schema defaults/nullability failed", e)
	}
}
