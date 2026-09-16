package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/JC-SYSU/pubmed-surfing/internal/pubmed"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type schema = map[string]any

// Nullable values use JSON Schema's type array rather than anyOf. This is a
// semantically equivalent, broadly supported JSON Schema representation.
func nullable(t string) any { return schema{"type": []string{t, "null"}} }
func arrNullable() any {
	return schema{"type": []string{"array", "null"}, "items": schema{"type": "string"}}
}
func field(t string, def any) schema { return schema{"type": t, "default": def} }
func obj(props schema, required ...string) schema {
	s := schema{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

var broadOutput = schema{"type": "object", "additionalProperties": true}
var listOutput = schema{"type": "object", "properties": schema{"result": schema{"type": "array", "items": schema{"type": "object", "additionalProperties": true}}}, "required": []string{"result"}, "additionalProperties": false}
var scalarOutput = schema{"type": "object", "properties": schema{"result": schema{"type": "integer"}}, "required": []string{"result"}, "additionalProperties": false}

type definition struct {
	name, description string
	input, output     schema
}

func definitions() []definition {
	return []definition{
		{"pubmed_search", "Search PubMed via NCBI E-utilities using raw, structured, or proximity queries. retmax is capped at 100 by default; if the query's total_count exceeds the cap and you need more results, ask the user whether to fetch the full set (up to 10000), then retry with confirm_full=true.", obj(schema{"query": field("string", ""), "retmax": field("integer", 10), "confirm_full": field("boolean", false), "title": withDefault(arrNullable(), nil), "abstract": withDefault(arrNullable(), nil), "all_field": withDefault(arrNullable(), nil), "mesh": withDefault(arrNullable(), nil), "mesh_major": withDefault(arrNullable(), nil), "mesh_no_expand": withDefault(arrNullable(), nil), "issn": withDefault(nullable("string"), nil), "journal_name": withDefault(nullable("string"), nil), "journal": withDefault(nullable("string"), nil), "author": withDefault(arrNullable(), nil), "year_from": withDefault(nullable("integer"), nil), "year_to": withDefault(nullable("integer"), nil), "article_type": withDefault(nullable("string"), nil), "language": withDefault(nullable("string"), nil), "proximity_terms": withDefault(arrNullable(), nil), "proximity_field": field("string", "Title/Abstract"), "proximity_distance": field("integer", 5), "sort": withDefault(nullable("string"), nil), "field_join": field("string", "OR")}), broadOutput},
		{"pubmed_get_total_count", "Return the total PubMed record count for a query without fetching paper details.", obj(schema{"query": schema{"type": "string"}}, "query"), scalarOutput},
		{"pubmed_clear_cache", "Clear PubMed Surfing's in-process request cache.", obj(schema{}), broadOutput},
		{"pubmed_fetch_abstract", "Fetch full abstract and metadata for one PubMed article by PMID.", obj(schema{"pmid": schema{"type": "string"}}, "pmid"), broadOutput},
		{"pubmed_fetch_abstracts_batch", "Fetch abstracts for multiple PMIDs with simple rate limiting.", obj(schema{"pmids": schema{"type": "array", "items": schema{"type": "string"}}, "delay": field("number", 0.5), "max_items": field("integer", 5), "max_abstract_chars": field("integer", 1800)}, "pmids"), listOutput},
		{"pubmed_verify_article_type", "Use PubMed history to check whether a journal has published an article type.", obj(schema{"issn": schema{"type": "string"}, "article_type": field("string", "case report"), "years": field("integer", 6), "topic_keywords": withDefault(arrNullable(), nil)}, "issn"), broadOutput},
		{"pubmed_find_related", "Find PubMed related articles for one PMID and summarize likely journals.", obj(schema{"pmid": schema{"type": "string"}, "retmax": field("integer", 20), "include_abstracts": field("boolean", false), "max_abstract_chars": field("integer", 1000), "filter_query": withDefault(nullable("string"), nil)}, "pmid"), broadOutput},
		{"pubmed_convert_ids", "Convert among DOI, PMID, and PMCID using NCBI PMC ID Converter.", obj(schema{"ids": schema{"type": "array", "items": schema{"type": "string"}}, "id_type": withDefault(nullable("string"), nil), "include_versions": field("boolean", false)}, "ids"), broadOutput},
		{"pubmed_format_citations", "Format PubMed records as APA, MLA, BibTeX, or RIS citations.", obj(schema{"pmids": withDefault(arrNullable(), nil), "papers": withDefault(schema{"type": []string{"array", "null"}, "items": schema{"type": "object", "additionalProperties": true}}, nil), "style": field("string", "apa"), "max_items": field("integer", 20), "include_abstracts": field("boolean", false)}), broadOutput},
		{"pubmed_journal_profile", "Get article type distribution for a journal by ISSN.\n\nReturns total article count plus per-type counts (Review, Case Reports,\nMeta-Analysis, etc.) for the given time window, helping assess a journal's\npublication preferences at a glance.", obj(schema{"issn": schema{"type": "string"}, "years": field("integer", 6), "article_types": withDefault(arrNullable(), nil)}, "issn"), broadOutput},
		{"pubmed_journal_mesh_profile", "Get top MeSH descriptors for recent PubMed records in a journal by ISSN.", obj(schema{"issn": schema{"type": "string"}, "years": field("integer", 6), "sample_size": field("integer", 100), "top_n": field("integer", 20)}, "issn"), broadOutput},
	}
}
func withDefault(v any, d any) schema { s := v.(schema); s["default"] = d; return s }

func tool(d definition) *mcp.Tool {
	return &mcp.Tool{Name: d.name, Description: d.description, InputSchema: d.input, OutputSchema: d.output}
}

func jsonText(v any) string {
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	_ = e.Encode(v)
	return strings.TrimSuffix(b.String(), "\n")
}

func addObject[In any](srv *mcp.Server, d definition, h func(context.Context, In) pubmed.Record) {
	mcp.AddTool(srv, tool(d), func(ctx context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, pubmed.Record, error) {
		out := h(ctx, in)
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: jsonText(out)}}}, out, nil
	})
}

type scalarResult struct {
	Result int `json:"result"`
}
type listResult struct {
	Result []pubmed.Record `json:"result"`
}

func New(service *pubmed.Service) *mcp.Server {
	if service == nil {
		service = pubmed.NewService(pubmed.Options{})
	}
	srv := mcp.NewServer(&mcp.Implementation{Name: "PubMed Surfing", Version: pubmed.Version}, nil)
	defs := map[string]definition{}
	for _, d := range definitions() {
		defs[d.name] = d
	}
	addObject(srv, defs["pubmed_search"], func(ctx context.Context, in pubmed.SearchInput) pubmed.Record { return service.ExecuteSearch(ctx, in) })
	type queryInput struct {
		Query string `json:"query"`
	}
	mcp.AddTool(srv, tool(defs["pubmed_get_total_count"]), func(ctx context.Context, _ *mcp.CallToolRequest, in queryInput) (*mcp.CallToolResult, scalarResult, error) {
		n := service.GetTotalCount(ctx, in.Query)
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprint(n)}}}, scalarResult{n}, nil
	})
	addObject(srv, defs["pubmed_clear_cache"], func(context.Context, struct{}) pubmed.Record { return service.ClearCache() })
	type pmidInput struct {
		PMID string `json:"pmid"`
	}
	addObject(srv, defs["pubmed_fetch_abstract"], func(ctx context.Context, in pmidInput) pubmed.Record { return service.FetchAbstract(ctx, in.PMID) })
	mcp.AddTool(srv, tool(defs["pubmed_fetch_abstracts_batch"]), func(ctx context.Context, _ *mcp.CallToolRequest, in pubmed.BatchAbstractInput) (*mcp.CallToolResult, listResult, error) {
		items := service.FetchAbstractsBatch(ctx, in)
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: jsonText(items)}}}, listResult{items}, nil
	})
	addObject(srv, defs["pubmed_verify_article_type"], func(ctx context.Context, in pubmed.VerifyArticleTypeInput) pubmed.Record {
		return service.VerifyArticleType(ctx, in)
	})
	addObject(srv, defs["pubmed_find_related"], func(ctx context.Context, in pubmed.FindRelatedInput) pubmed.Record {
		return service.FindRelated(ctx, in)
	})
	addObject(srv, defs["pubmed_convert_ids"], func(ctx context.Context, in pubmed.ConvertIDsInput) pubmed.Record { return service.ConvertIDs(ctx, in) })
	addObject(srv, defs["pubmed_format_citations"], func(ctx context.Context, in pubmed.FormatCitationsInput) pubmed.Record {
		return service.FormatCitations(ctx, in)
	})
	addObject(srv, defs["pubmed_journal_profile"], func(ctx context.Context, in pubmed.JournalProfileInput) pubmed.Record {
		return service.JournalProfile(ctx, in)
	})
	addObject(srv, defs["pubmed_journal_mesh_profile"], func(ctx context.Context, in pubmed.JournalMeshProfileInput) pubmed.Record {
		return service.JournalMeshProfile(ctx, in)
	})
	return srv
}
