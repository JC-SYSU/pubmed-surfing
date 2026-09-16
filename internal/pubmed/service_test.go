package pubmed

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

type doerFunc func(*http.Request) (*http.Response, error)

func intp(n int) *int { return &n }

func (f doerFunc) Do(r *http.Request) (*http.Response, error) { return f(r) }
func response(body string, status ...int) *http.Response {
	n := 200
	if len(status) > 0 {
		n = status[0]
	}
	return &http.Response{StatusCode: n, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}
}

const fixtureXML = `<PubmedArticleSet><PubmedArticle><MedlineCitation><PMID>123</PMID><Article><ArticleTitle>Effect of <i>Drug A</i> on <b>outcome</b> tail</ArticleTitle><Abstract><AbstractText Label="BACKGROUND">A <sup>2</sup> B <i>x</i></AbstractText><AbstractText>plain &amp; text</AbstractText></Abstract><AuthorList><Author><LastName>Smith</LastName><ForeName>Jane</ForeName></Author></AuthorList><Journal><Title>Sample Journal</Title><ISOAbbreviation>Samp J</ISOAbbreviation><ISSN IssnType="Electronic">1234-5678</ISSN><JournalIssue><Volume>2</Volume><Issue>3</Issue><PubDate><Year>2025</Year></PubDate></JournalIssue></Journal><Pagination><MedlinePgn>10-12</MedlinePgn></Pagination><ELocationID EIdType="doi">10.1/example</ELocationID></Article><MeshHeadingList><MeshHeading><DescriptorName>Humans</DescriptorName><QualifierName>genetics</QualifierName></MeshHeading></MeshHeadingList></MedlineCitation></PubmedArticle></PubmedArticleSet>`

func serviceFor(t *testing.T, route func(*url.URL) (string, int, error)) *Service {
	t.Helper()
	d := doerFunc(func(r *http.Request) (*http.Response, error) {
		body, status, e := route(r.URL)
		if e != nil {
			return nil, e
		}
		return response(body, status), nil
	})
	return NewService(Options{HTTP: NewHTTPClient(d, nil), Now: func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }, Sleep: func(context.Context, time.Duration) error { return nil }})
}

func TestQueryContracts(t *testing.T) {
	title := []string{"Meta Analysis"}
	mesh := []string{"Acute Kidney Injury"}
	issn := "1536-5964"
	from, to := 2020, 2025
	typ := "Meta-Analysis"
	q := BuildPubmedQuery(SearchInput{Title: &title, Mesh: &mesh, ISSN: &issn, YearFrom: &from, YearTo: &to, ArticleType: &typ, FieldJoin: "OR"})
	want := `("Meta Analysis"[Title]) AND ("Acute Kidney Injury"[mh]) AND 1536-5964[ta] AND (2020[dp] : 2025[dp]) AND "Meta-Analysis"[ptyp]`
	if q != want {
		t.Fatalf("query\n got %s\nwant %s", q, want)
	}
	if got := BuildPubmedProximityQuery([]string{"rationing", "healthcare"}, "tiab", 5); got != `"rationing healthcare"[Title/Abstract:~5]` {
		t.Fatal(got)
	}
	if CoerceRetmax(250) != 100 || CoerceRetmax(-1) != 0 {
		t.Fatal("retmax bounds")
	}
}

func TestCacheContracts(t *testing.T) {
	now := time.Unix(0, 0)
	c := NewCache(func() time.Time { return now })
	p1 := url.Values{"b": {"2"}, "a": {"1"}}
	p2 := url.Values{"a": {"1"}, "b": {"2"}}
	if CacheKey("x", p1) != CacheKey("x", p2) {
		t.Fatal("unstable key")
	}
	c.Set("x", []byte("one"))
	b, ok := c.Get("x")
	b[0] = 'X'
	b2, _ := c.Get("x")
	if !ok || string(b2) != "one" {
		t.Fatal("cache clone")
	}
	now = now.Add(CacheTTL)
	if _, ok = c.Get("x"); ok {
		t.Fatal("expired cache")
	}
	for i := 0; i < CacheMaxItems+1; i++ {
		c.Set(string(rune(i)), []byte("x"))
	}
	if len(c.items) != CacheMaxItems {
		t.Fatal("capacity")
	}
	if c.Clear() != CacheMaxItems {
		t.Fatal("clear count")
	}
}

func TestXMLAndTruncationContracts(t *testing.T) {
	a, e := ParseArticles([]byte(fixtureXML))
	if e != nil || len(a) != 1 {
		t.Fatal(e)
	}
	r := articleRecord(a[0], "")
	if r["title"] != "Effect of Drug A on outcome tail" || r["abstract"] != "BACKGROUND: A 2 B x\nplain & text" || r["authors"] != "Smith Jane" || r["doi"] != "10.1/example" || r["mesh_terms"] != "Humans/genetics" {
		t.Fatalf("bad parse: %#v", r)
	}
	issn, eissn := ExtractJournalISSNs(a[0])
	if issn != "" || eissn != "1234-5678" {
		t.Fatal(issn, eissn)
	}
	long := Record{"abstract": strings.Repeat("A", 700), "abstract_sections": []Record{{"label": "METHODS", "text": strings.Repeat("m", 300)}, {"label": "CONCLUSIONS", "text": strings.Repeat("c", 300)}}}
	tr := TruncateAbstractItem(long, 500)
	if tr["abstract_truncation_strategy"] != "section-aware" || len(tr["abstract"].(string)) > 500 {
		t.Fatal(tr)
	}
}

func TestSearchSuccessPartialAndEmpty(t *testing.T) {
	calls := 0
	s := serviceFor(t, func(u *url.URL) (string, int, error) {
		calls++
		if strings.Contains(u.Path, "esearch") {
			if u.Query().Get("retmax") != "100" {
				t.Fatalf("expected capped retmax=100, got %s", u.Query().Get("retmax"))
			}
			return `{"esearchresult":{"count":"250","idlist":["1","2"]}}`, 200, nil
		}
		if strings.Contains(u.Path, "esummary") {
			return `{"result":{"1":{"title":"First","source":"J","pubdate":"2025 Jan","authors":[{"name":"A A"}]},"2":{"title":"Second","fulljournalname":"K","pubdate":"2024"}}}`, 200, nil
		}
		return "", 500, nil
	})
	r := s.ExecuteSearch(context.Background(), SearchInput{Query: "cancer", Retmax: intp(250)})
	if r["total_count"] != 250 || r["partial"] != false || len(r["papers"].([]Record)) != 2 {
		t.Fatal(r)
	}
	if r["truncated"] != true || r["requested_retmax"] != 250 || r["returned_papers"] != 100 {
		t.Fatalf("truncation metadata missing: %v", r)
	}
	full := serviceFor(t, func(u *url.URL) (string, int, error) {
		if strings.Contains(u.Path, "esearch") {
			if got := u.Query().Get("retmax"); got != "250" {
				t.Fatalf("expected uncapped retmax=250 with confirm_full, got %s", got)
			}
			return `{"esearchresult":{"count":"250","idlist":["1","2"]}}`, 200, nil
		}
		return `{"result":{"1":{"title":"First","source":"J","pubdate":"2025 Jan"},"2":{"title":"Second","pubdate":"2024"}}}`, 200, nil
	}).ExecuteSearch(context.Background(), SearchInput{Query: "cancer", Retmax: intp(250), ConfirmFull: true})
	if full["truncated"] != nil || len(full["papers"].([]Record)) != 2 {
		t.Fatal(full)
	}
	empty := s.ExecuteSearch(context.Background(), SearchInput{})
	if empty["error"] == nil {
		t.Fatal(empty)
	}
	partial := serviceFor(t, func(u *url.URL) (string, int, error) {
		if strings.Contains(u.Path, "esearch") {
			return `{"esearchresult":{"count":"2","idlist":["1","2"]}}`, 200, nil
		}
		return "", 0, errors.New("slow summary")
	}).SearchPubmed(context.Background(), "x", 2, nil, false)
	if partial["partial"] != true || len(partial["papers"].([]Record)) != 2 {
		t.Fatal(partial)
	}
	_ = calls
}

func TestExplicitZeroPresenceSemantics(t *testing.T) {
	zero := 0
	s := serviceFor(t, func(u *url.URL) (string, int, error) {
		if strings.Contains(u.Path, "esearch") {
			if u.Query().Get("retmax") != "0" {
				t.Fatalf("explicit retmax zero lost: %s", u.RawQuery)
			}
			return `{"esearchresult":{"count":"0","idlist":[]}}`, 200, nil
		}
		return "", 500, nil
	})
	s.ExecuteSearch(context.Background(), SearchInput{Query: "x", Retmax: &zero})
	batch := s.FetchAbstractsBatch(context.Background(), BatchAbstractInput{PMIDs: []string{"1"}, MaxItems: &zero})
	if len(batch) != 1 || batch[0]["omitted"] != "1" {
		t.Fatal(batch)
	}
	profile := s.VerifyArticleType(context.Background(), VerifyArticleTypeInput{ISSN: "x", Years: &zero})
	if strings.Contains(profile["query"].(string), "[dp]") {
		t.Fatal(profile)
	}
}

func TestFetchAndBatchContracts(t *testing.T) {
	s := serviceFor(t, func(u *url.URL) (string, int, error) {
		if u.Query().Get("id") == "bad" {
			return "", 0, errors.New("boom")
		}
		return fixtureXML, 200, nil
	})
	if s.FetchAbstract(context.Background(), "123")["title"] == nil {
		t.Fatal("missing article")
	}
	if s.FetchAbstract(context.Background(), "bad")["error"] == nil {
		t.Fatal("missing error")
	}
	r := s.FetchAbstractsBatch(context.Background(), BatchAbstractInput{PMIDs: []string{"123", "123", "missing"}, MaxItems: intp(2), MaxAbstractChars: 500})
	if len(r) != 3 || r[0]["pmid"] != "123" || r[1]["pmid"] != "123" || r[2]["omitted"] != "1" {
		t.Fatal(r)
	}
}

func TestRelatedAndConverterContracts(t *testing.T) {
	s := serviceFor(t, func(u *url.URL) (string, int, error) {
		switch {
		case strings.Contains(u.Path, "elink"):
			return `{"linksets":[{"linksetdbs":[{"dbto":"pubmed","links":[{"id":"123","score":99},{"id":"456","score":8},{"id":"456","score":7}]}]}]}`, 200, nil
		case strings.Contains(u.Path, "efetch"):
			return strings.ReplaceAll(fixtureXML, "<PMID>123</PMID>", "<PMID>456</PMID>"), 200, nil
		case strings.Contains(u.Path, "idconv"):
			return `{"status":"ok","records":[{"requested-id":"23903748","pmid":23903748,"pmcid":"PMC1"}],"request":{"warnings":["w"]}}`, 200, nil
		}
		return "", 500, nil
	})
	rel := s.FindRelated(context.Background(), FindRelatedInput{PMID: "123", Retmax: intp(20)})
	if rel["related_count"] != 1 {
		t.Fatal(rel)
	}
	ids := s.ConvertIDs(context.Background(), ConvertIDsInput{IDs: []string{"23903748", "missing"}, IncludeVersions: true})
	if ids["converted_count"] != 1 || len(ids["unresolved_ids"].([]string)) != 1 {
		t.Fatal(ids)
	}
	bad := "wat"
	if s.ConvertIDs(context.Background(), ConvertIDsInput{IDs: []string{"x"}, IDType: &bad})["error"] == nil {
		t.Fatal("invalid type")
	}
}

func TestCitationStyles(t *testing.T) {
	papers := []Record{{"pmid": "123", "title": "Paper title", "authors": "Smith Jane; Doe John", "journal": "Journal A", "year": "2025", "doi": "10.1/example", "abstract": "Compact abstract"}}
	s := NewService(Options{})
	for _, style := range []string{"apa", "mla", "bibtex", "ris"} {
		r := s.FormatCitations(context.Background(), FormatCitationsInput{Papers: &papers, Style: style, IncludeAbstracts: true})
		if r["count"] != 1 || r["formatted"] == "" {
			t.Fatal(style, r)
		}
	}
	bad := s.FormatCitations(context.Background(), FormatCitationsInput{Papers: &papers, Style: "bad"})
	if len(bad["errors"].([]Record)) != 1 {
		t.Fatal(bad)
	}
}

func TestProfilesAndMeshContracts(t *testing.T) {
	var mu sync.Mutex
	active, maxActive := 0, 0
	s := serviceFor(t, func(u *url.URL) (string, int, error) {
		switch {
		case strings.Contains(u.Path, "esearch"):
			mu.Lock()
			active++
			if active > maxActive {
				maxActive = active
			}
			mu.Unlock()
			time.Sleep(time.Millisecond)
			mu.Lock()
			active--
			mu.Unlock()
			if u.Query().Get("retmax") == "100" && u.Query().Get("sort") == "date" {
				return `{"esearchresult":{"count":"2","idlist":["123"]}}`, 200, nil
			}
			return `{"esearchresult":{"count":"3","idlist":[]}}`, 200, nil
		case strings.Contains(u.Path, "efetch"):
			return fixtureXML, 200, nil
		}
		return "", 500, nil
	})
	types := []string{"review", "meta analysis", "case report", "letter"}
	p := s.JournalProfile(context.Background(), JournalProfileInput{ISSN: "1536-5964", Years: intp(6), ArticleTypes: &types})
	if p["total_articles"] != 3 || maxActive > 3 {
		t.Fatal(p, maxActive)
	}
	m := s.JournalMeshProfile(context.Background(), JournalMeshProfileInput{ISSN: "1536-5964", Years: intp(6), SampleSize: 100, TopN: 20})
	if m["articles_with_mesh"] != 1 || m["mesh_terms"].(Record)["Humans"] != 1 {
		t.Fatal(m)
	}
	v := s.VerifyArticleType(context.Background(), VerifyArticleTypeInput{ISSN: "1536-5964", ArticleType: "case report", Years: intp(6)})
	if !strings.Contains(v["query"].(string), `"Case Reports"[ptyp]`) {
		t.Fatal(v)
	}
}
