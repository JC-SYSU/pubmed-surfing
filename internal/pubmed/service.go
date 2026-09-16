package pubmed

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	esearchURL     = "https://eutils.ncbi.nlm.nih.gov/entrez/eutils/esearch.fcgi"
	esummaryURL    = "https://eutils.ncbi.nlm.nih.gov/entrez/eutils/esummary.fcgi"
	efetchURL      = "https://eutils.ncbi.nlm.nih.gov/entrez/eutils/efetch.fcgi"
	elinkURL       = "https://eutils.ncbi.nlm.nih.gov/entrez/eutils/elink.fcgi"
	idConverterURL = "https://www.ncbi.nlm.nih.gov/pmc/utils/idconv/v1.0/"
)

var publicationTypes = map[string]string{"case report": "Case Reports", "case reports": "Case Reports", "review": "Review", "systematic review": "Systematic Review", "meta analysis": "Meta-Analysis", "meta-analysis": "Meta-Analysis", "clinical trial": "Clinical Trial", "randomized controlled trial": "Randomized Controlled Trial", "editorial": "Editorial", "letter": "Letter", "guideline": "Guideline"}
var defaultProfileTypes = []string{"Case Reports", "Clinical Trial", "Editorial", "Guideline", "Letter", "Meta-Analysis", "Randomized Controlled Trial", "Review", "Systematic Review"}

type Service struct {
	http  *HTTPClient
	now   func() time.Time
	sleep func(context.Context, time.Duration) error
}

func NewService(o Options) *Service {
	h := o.HTTP
	if h == nil {
		h = NewHTTPClient(nil, nil)
	}
	now := o.Now
	if now == nil {
		now = time.Now
	}
	sl := o.Sleep
	if sl == nil {
		sl = func(ctx context.Context, d time.Duration) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(d):
				return nil
			}
		}
	}
	return &Service{h, now, sl}
}
func (s *Service) ClearCache() Record { return Record{"cleared": s.http.ClearCache()} }
func vals(pairs ...string) url.Values {
	v := url.Values{}
	for i := 0; i+1 < len(pairs); i += 2 {
		v.Set(pairs[i], pairs[i+1])
	}
	return v
}
func asMap(v any) Record {
	if r, ok := v.(map[string]any); ok {
		return r
	}
	if r, ok := v.(Record); ok {
		return r
	}
	return Record{}
}
func asSlice(v any) []any {
	if a, ok := v.([]any); ok {
		return a
	}
	return nil
}
func intValue(v any) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case json.Number:
		n, _ := strconv.Atoi(x.String())
		return n
	case string:
		n, _ := strconv.Atoi(x)
		return n
	case int:
		return x
	}
	return 0
}
func str(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}
func pmids(data Record) []string {
	r := asMap(data["esearchresult"])
	out := []string{}
	for _, v := range asSlice(r["idlist"]) {
		out = append(out, str(v))
	}
	return out
}
func total(data Record) int { return intValue(asMap(data["esearchresult"])["count"]) }
func summaryPapers(data Record, ids []string) []Record {
	res := asMap(data["result"])
	out := []Record{}
	for _, id := range ids {
		r := asMap(res[id])
		if len(r) == 0 {
			continue
		}
		authors := []string{}
		for _, a := range asSlice(r["authors"]) {
			if n := str(asMap(a)["name"]); n != "" {
				authors = append(authors, n)
			}
		}
		journal := str(r["source"])
		if journal == "" {
			journal = str(r["fulljournalname"])
		}
		year := ""
		if f := strings.Fields(str(r["pubdate"])); len(f) > 0 {
			year = f[0]
		}
		out = append(out, Record{"title": str(r["title"]), "journal": journal, "year": year, "authors": strings.Join(authors, "; "), "pmid": id, "url": "https://pubmed.ncbi.nlm.nih.gov/" + id + "/"})
	}
	return out
}

func (s *Service) ExecuteSearch(ctx context.Context, in SearchInput) Record {
	defaultsSearch(&in)
	q := in.Query
	if in.ProximityTerms != nil && len(*in.ProximityTerms) > 0 {
		q = BuildPubmedProximityQuery(*in.ProximityTerms, in.ProximityField, *in.ProximityDistance)
	} else if structured(in) {
		q = BuildPubmedQuery(in)
	}
	if q == "" {
		return Record{"source": "pubmed", "query": "", "papers": []Record{}, "total_count": 0, "partial": false, "error": "Provide a raw query, structured PubMed fields, or proximity_terms."}
	}
	r := s.SearchPubmed(ctx, q, *in.Retmax, in.Sort, in.ConfirmFull)
	out := Record{"source": "pubmed", "query": q, "papers": r["papers"], "total_count": r["total_count"], "partial": r["partial"]}
	for _, k := range []string{"note", "error", "requested_retmax", "returned_papers", "truncated"} {
		if v := r[k]; v != nil && v != "" {
			out[k] = v
		}
	}
	return out
}
func structured(in SearchInput) bool {
	return nonempty(in.Title) || nonempty(in.Abstract) || nonempty(in.AllField) || nonempty(in.Mesh) || nonempty(in.MeshMajor) || nonempty(in.MeshNoExpand) || nonemptyS(in.ISSN) || nonemptyS(in.JournalName) || nonemptyS(in.Journal) || nonempty(in.Author) || in.YearFrom != nil || in.YearTo != nil || nonemptyS(in.ArticleType) || nonemptyS(in.Language)
}
func nonempty(p *[]string) bool { return p != nil && len(*p) > 0 }
func nonemptyS(p *string) bool  { return p != nil && *p != "" }

// Request-size bounds for esummary. LAYER 2 of the size limits: pure
// transport, how many UIDs one HTTP request may carry — unrelated to
// DefaultRetmax (LAYER 1, how many papers a search returns; see query.go).
//
// esummaryGetLimit is the largest multi-ID esummary sent as GET. NCBI's
// official ESummary guidance (https://www.ncbi.nlm.nih.gov/books/NBK25499/)
// is to switch to HTTP POST above ~200 UIDs.
//
// POST itself is still bounded: NCBI caps esummary at 500 UIDs per request
// in JSON mode ("Too many UIDs in request. Maximum number of UIDs is 500" —
// verified experimentally; not in the official docs), so larger fetches are
// split into 500-ID POST batches and merged.
const esummaryGetLimit = EsummaryPostThreshold
const esummaryPostLimit = 500

func (s *Service) fetchSummaries(ctx context.Context, ids []string) (Record, error) {
	if len(ids) <= esummaryGetLimit {
		p := vals("db", "pubmed", "id", strings.Join(ids, ","), "retmode", "json")
		summ, e := s.http.JSON(ctx, esummaryURL, p, EutilsTimeout)
		if e != nil {
			return nil, e
		}
		return Record{"result": asMap(summ["result"])}, nil
	}
	merged := Record{}
	for start := 0; start < len(ids); start += esummaryPostLimit {
		end := start + esummaryPostLimit
		if end > len(ids) {
			end = len(ids)
		}
		p := vals("db", "pubmed", "id", strings.Join(ids[start:end], ","), "retmode", "json")
		summ, e := s.http.JSONPost(ctx, esummaryURL, p, EutilsTimeout)
		if e != nil {
			return nil, e
		}
		for k, v := range asMap(summ["result"]) {
			merged[k] = v
		}
	}
	return Record{"result": merged}, nil
}

func (s *Service) SearchPubmed(ctx context.Context, q string, retmax int, sortArg *string, confirmFull bool) Record {
	requested := retmax
	n := CoerceRetmax(retmax)
	if confirmFull {
		if requested < 0 {
			requested = 0
		}
		if requested > HardRetmax {
			requested = HardRetmax
		}
		n = requested
	}
	p := vals("db", "pubmed", "term", q, "retmax", strconv.Itoa(n), "retmode", "json")
	if sortArg != nil && *sortArg != "" {
		p.Set("sort", *sortArg)
	}
	data, e := s.http.JSON(ctx, esearchURL, p, EutilsTimeout)
	if e != nil {
		return Record{"papers": []Record{}, "total_count": 0, "query": q, "error": "PubMed esearch failed or timed out: " + e.Error(), "partial": false}
	}
	ids := pmids(data)
	count := total(data)
	if len(ids) == 0 {
		return Record{"papers": []Record{}, "total_count": count, "query": q, "partial": false}
	}
	if len(ids) > n {
		ids = ids[:n]
	}
	summ, e := s.fetchSummaries(ctx, ids)
	if e != nil {
		papers := []Record{}
		for _, id := range ids {
			papers = append(papers, Record{"title": "", "journal": "", "year": "", "authors": "", "pmid": id, "url": "https://pubmed.ncbi.nlm.nih.gov/" + id + "/", "summary_status": "not_fetched"})
		}
		return Record{"papers": papers, "total_count": count, "query": q, "partial": true, "note": "PubMed esummary timed out; returned PMID-only results. Fetch abstracts by PMID if needed. Error: " + e.Error()}
	}
	out := Record{"papers": summaryPapers(summ, ids), "total_count": count, "query": q, "partial": false}
	if !confirmFull && requested > n {
		// The 100 cap is a product decision (see DefaultRetmax), not an NCBI
		// limit — hence the explicit confirmation handshake instead of
		// silently truncating or silently fetching more.
		out["note"] = fmt.Sprintf("retmax %d exceeds the %d-result default cap; returning at most %d. To retrieve more (up to %d), ask the user whether to fetch the full set and retry with confirm_full=true.", requested, n, n, HardRetmax)
		out["requested_retmax"] = requested
		out["returned_papers"] = n
		out["truncated"] = true
	}
	return out
}
func (s *Service) GetTotalCount(ctx context.Context, q string) int {
	n, e := s.GetTotalCountStrict(ctx, q)
	if e != nil {
		return 0
	}
	return n
}
func (s *Service) GetTotalCountStrict(ctx context.Context, q string) (int, error) {
	data, e := s.http.JSON(ctx, esearchURL, vals("db", "pubmed", "term", q, "retmax", "0", "retmode", "json"), DefaultTimeout)
	if e != nil {
		return 0, e
	}
	return total(data), nil
}
func (s *Service) SearchIDs(ctx context.Context, q string, n int, sortArg string) ([]string, int, error) {
	p := vals("db", "pubmed", "term", q, "retmax", strconv.Itoa(CoerceRetmax(n)), "retmode", "json")
	if sortArg != "" {
		p.Set("sort", sortArg)
	}
	d, e := s.http.JSON(ctx, esearchURL, p, EutilsTimeout)
	if e != nil {
		return nil, 0, e
	}
	return pmids(d), total(d), nil
}
func (s *Service) FetchArticles(ctx context.Context, ids []string) ([]xmlArticle, error) {
	clean := []string{}
	for _, id := range ids {
		if id = strings.TrimSpace(id); id != "" {
			clean = append(clean, id)
		}
	}
	if len(clean) == 0 {
		return []xmlArticle{}, nil
	}
	b, e := s.http.XML(ctx, efetchURL, vals("db", "pubmed", "id", strings.Join(clean, ","), "retmode", "xml", "rettype", "abstract"), DefaultTimeout, true)
	if e != nil {
		return nil, e
	}
	return ParseArticles(b)
}
func recordsByPMID(a []xmlArticle) map[string]Record {
	m := map[string]Record{}
	for _, x := range a {
		r := articleRecord(x, "")
		m[str(r["pmid"])] = r
	}
	return m
}
func (s *Service) FetchAbstract(ctx context.Context, id string) Record {
	clean := fmt.Sprint(id)
	b, e := s.http.XML(ctx, efetchURL, vals("db", "pubmed", "id", clean, "retmode", "xml", "rettype", "abstract"), DefaultTimeout, false)
	if e != nil {
		return Record{"pmid": clean, "error": e.Error()}
	}
	a, e := ParseArticles(b)
	if e != nil {
		return Record{"pmid": clean, "error": e.Error()}
	}
	if len(a) == 0 {
		return Record{"pmid": clean, "error": "Article not found in efetch response"}
	}
	return articleRecord(a[0], clean)
}
func (s *Service) FetchAbstractsBatch(ctx context.Context, in BatchAbstractInput) []Record {
	max := 5
	if in.MaxItems != nil {
		max = *in.MaxItems
	}
	if max < 0 {
		max = 0
	}
	selected := in.PMIDs
	if len(selected) > max {
		selected = selected[:max]
	}
	clean := make([]string, len(selected))
	for i, x := range selected {
		clean[i] = strings.TrimSpace(x)
	}
	out := []Record{}
	a, e := s.FetchArticles(ctx, clean)
	if e != nil {
		for _, id := range clean {
			out = append(out, Record{"pmid": id, "error": e.Error()})
		}
	} else {
		m := recordsByPMID(a)
		limit := in.MaxAbstractChars
		if limit == 0 {
			limit = 1800
		}
		for _, id := range clean {
			r := m[id]
			if r == nil {
				r = Record{"pmid": id, "error": "Article not found in efetch response"}
			}
			out = append(out, TruncateAbstractItem(cloneRecord(r), limit))
		}
	}
	if omitted := len(in.PMIDs) - len(selected); omitted > 0 {
		out = append(out, Record{"pmid": "", "note": fmt.Sprintf("Batch capped at %d PMIDs to keep MCP output compact.", len(selected)), "omitted": strconv.Itoa(omitted)})
	}
	return out
}

func (s *Service) relatedLinks(ctx context.Context, id string, filter *string) ([]RelatedLink, error) {
	p := vals("dbfrom", "pubmed", "db", "pubmed", "id", id, "cmd", "neighbor_score", "retmode", "json")
	if filter != nil && *filter != "" {
		p.Set("term", *filter)
	}
	d, e := s.http.JSON(ctx, elinkURL, p, EutilsTimeout)
	if e != nil {
		return nil, e
	}
	out := []RelatedLink{}
	seen := map[string]bool{}
	for _, ls := range asSlice(d["linksets"]) {
		for _, db := range asSlice(asMap(ls)["linksetdbs"]) {
			dm := asMap(db)
			if strings.ToLower(str(dm["dbto"])) != "pubmed" {
				continue
			}
			for _, l := range asSlice(dm["links"]) {
				lm := asMap(l)
				rid := strings.TrimSpace(str(lm["id"]))
				if rid == "" {
					rid = strings.TrimSpace(str(l))
				}
				if rid == "" || rid == id || seen[rid] {
					continue
				}
				seen[rid] = true
				var score *int
				if lm["score"] != nil {
					n := intValue(lm["score"])
					score = &n
				}
				out = append(out, RelatedLink{rid, score})
			}
		}
	}
	return out, nil
}
func (s *Service) FindRelated(ctx context.Context, in FindRelatedInput) Record {
	id := strings.TrimSpace(in.PMID)
	q := Record{"dbfrom": "pubmed", "db": "pubmed", "cmd": "neighbor_score", "id": id, "filter_query": nil}
	if in.FilterQuery != nil {
		q["filter_query"] = *in.FilterQuery
	}
	empty := func() Record {
		return Record{"source_pmid": id, "related_count": 0, "related_articles": []Record{}, "journal_counts": Record{}, "query": q}
	}
	if id == "" {
		r := empty()
		r["error"] = "pmid is required"
		return r
	}
	links, e := s.relatedLinks(ctx, id, in.FilterQuery)
	if e != nil {
		r := empty()
		r["error"] = "PubMed elink failed or timed out: " + e.Error()
		return r
	}
	n := 20
	if in.Retmax != nil {
		n = *in.Retmax
	}
	n = CoerceRetmax(n)
	if len(links) > n {
		links = links[:n]
	}
	ids := []string{}
	for _, l := range links {
		ids = append(ids, l.PMID)
	}
	if len(ids) == 0 {
		return empty()
	}
	a, e := s.FetchArticles(ctx, ids)
	if e != nil {
		r := empty()
		r["error"] = "PubMed efetch failed or timed out: " + e.Error()
		r["pmids"] = ids
		return r
	}
	m := recordsByPMID(a)
	out := []Record{}
	counts := map[string]int{}
	for i, id := range ids {
		src := m[id]
		var r Record
		if src == nil {
			r = Record{"pmid": id, "error": "Article not found in efetch response"}
		} else {
			r = cloneRecord(src)
		}
		if links[i].Score == nil {
			r["score"] = nil
		} else {
			r["score"] = *links[i].Score
		}
		r["url"] = "https://pubmed.ncbi.nlm.nih.gov/" + id + "/"
		delete(r, "mesh_terms")
		if in.IncludeAbstracts {
			limit := in.MaxAbstractChars
			if limit == 0 {
				limit = 1000
			}
			r = TruncateAbstractItem(r, limit)
		} else {
			delete(r, "abstract")
			delete(r, "abstract_truncated")
			delete(r, "abstract_length")
		}
		if j := strings.TrimSpace(str(r["journal"])); j != "" {
			counts[j]++
		}
		out = append(out, r)
	}
	jc := Record{}
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		return counts[keys[i]] > counts[keys[j]] || counts[keys[i]] == counts[keys[j]] && keys[i] < keys[j]
	})
	for _, k := range keys {
		jc[k] = counts[k]
	}
	return Record{"source_pmid": id, "related_count": len(out), "related_articles": out, "journal_counts": jc, "query": q}
}

func (s *Service) ConvertIDs(ctx context.Context, in ConvertIDsInput) Record {
	ids := []string{}
	for _, id := range in.IDs {
		if id = strings.TrimSpace(id); id != "" {
			ids = append(ids, id)
		}
		if len(ids) == 100 {
			break
		}
	}
	var typ any = nil
	if in.IDType != nil && strings.TrimSpace(*in.IDType) != "" {
		typ = strings.ToLower(strings.TrimSpace(*in.IDType))
	}
	q := Record{"ids": ids, "id_type": typ, "include_versions": in.IncludeVersions}
	base := func() Record {
		return Record{"status": "error", "converted_count": 0, "records": []Record{}, "unresolved_ids": ids, "query": q, "warnings": []any{}}
	}
	if len(ids) == 0 {
		r := base()
		r["unresolved_ids"] = []string{}
		r["error"] = "ids is required"
		return r
	}
	allowed := map[string]bool{"doi": true, "pmid": true, "pmcid": true, "mid": true}
	if typ != nil && !allowed[typ.(string)] {
		r := base()
		r["error"] = "id_type must be one of doi, pmid, pmcid, mid, or omitted"
		return r
	}
	p := vals("ids", strings.Join(ids, ","), "format", "json", "tool", "PubMedSurfing")
	if typ != nil {
		p.Set("idtype", typ.(string))
	}
	if in.IncludeVersions {
		p.Set("versions", "yes")
	}
	d, enc, e := s.http.JSONWithQuery(ctx, idConverterURL, p, EutilsTimeout)
	if e != nil {
		r := base()
		r["error"] = "PMC ID Converter failed or timed out: " + e.Error()
		return r
	}
	records := []Record{}
	converted := map[string]bool{}
	for _, raw := range asSlice(d["records"]) {
		src := asMap(raw)
		dst := Record{}
		for _, pair := range [][2]string{{"requested-id", "requested_id"}, {"pmid", "pmid"}, {"pmcid", "pmcid"}, {"doi", "doi"}, {"mid", "mid"}, {"versions", "versions"}} {
			if v, ok := src[pair[0]]; ok && v != nil {
				if pair[0] == "pmid" || pair[0] == "mid" {
					v = str(v)
				}
				dst[pair[1]] = v
			}
		}
		if dst["pmid"] != nil || dst["pmcid"] != nil || dst["doi"] != nil {
			converted[str(dst["requested_id"])] = true
		}
		records = append(records, dst)
	}
	unresolved := []string{}
	for _, id := range ids {
		if !converted[id] {
			unresolved = append(unresolved, id)
		}
	}
	q["encoded"] = enc
	warnings := asSlice(asMap(d["request"])["warnings"])
	status := str(d["status"])
	if status == "" {
		status = "ok"
	}
	return Record{"status": status, "converted_count": len(converted), "records": records, "unresolved_ids": unresolved, "query": q, "warnings": warnings}
}

func (s *Service) FormatCitations(ctx context.Context, in FormatCitationsInput) Record {
	style := strings.ToLower(strings.TrimSpace(in.Style))
	if style == "" {
		style = "apa"
	}
	max := in.MaxItems
	if max == 0 {
		max = 20
	}
	if max < 0 {
		max = 0
	}
	if max > 100 {
		max = 100
	}
	ids := []string{}
	if in.PMIDs != nil {
		for _, id := range *in.PMIDs {
			if id = strings.TrimSpace(id); id != "" {
				ids = append(ids, id)
			}
			if len(ids) == max {
				break
			}
		}
	}
	source := "papers"
	if len(ids) > 0 {
		source = "pmids"
	}
	q := Record{"pmids": ids, "source": source, "max_items": max, "include_abstracts": in.IncludeAbstracts, "style": style}
	base := func(errs []Record) Record {
		return Record{"style": style, "count": 0, "citations": []Record{}, "formatted": "", "errors": errs, "query": q}
	}
	if !map[string]bool{"apa": true, "mla": true, "bibtex": true, "ris": true}[style] {
		return base([]Record{{"error": "style must be one of apa, mla, bibtex, ris"}})
	}
	raw := []Record{}
	errs := []Record{}
	if len(ids) > 0 {
		a, e := s.FetchArticles(ctx, ids)
		if e != nil {
			return base([]Record{{"error": "PubMed efetch failed or timed out: " + e.Error()}})
		}
		m := recordsByPMID(a)
		for _, id := range ids {
			if r := m[id]; r != nil {
				raw = append(raw, r)
			} else {
				errs = append(errs, Record{"pmid": id, "error": "Article not found in efetch response"})
			}
		}
	} else if in.Papers != nil && len(*in.Papers) > 0 {
		raw = append(raw, (*in.Papers)...)
		if len(raw) > max {
			raw = raw[:max]
		}
	} else {
		return base([]Record{{"error": "Provide pmids or papers to format."}})
	}
	used := map[string]int{}
	cit := []Record{}
	texts := []string{}
	for _, r := range raw {
		c := normalizeCitation(r)
		if !in.IncludeAbstracts {
			c.Abstract = ""
		}
		text := FormatCitation(c, style, used)
		cit = append(cit, Record{"pmid": c.PMID, "citation": text})
		texts = append(texts, text)
	}
	return Record{"style": style, "count": len(cit), "citations": cit, "formatted": strings.Join(texts, "\n\n"), "errors": errs, "query": q}
}

func (s *Service) fetchSampleTitles(ctx context.Context, q string, count int) []string {
	d, e := s.http.JSON(ctx, esearchURL, vals("db", "pubmed", "term", q, "retmax", strconv.Itoa(count), "retmode", "json", "sort", "date"), DefaultTimeout)
	if e != nil {
		return []string{}
	}
	ids := pmids(d)
	if len(ids) == 0 {
		return []string{}
	}
	sum, e := s.http.JSON(ctx, esummaryURL, vals("db", "pubmed", "id", strings.Join(ids, ","), "retmode", "json"), DefaultTimeout)
	if e != nil {
		return []string{}
	}
	res := asMap(sum["result"])
	out := []string{}
	for _, id := range ids {
		r := asMap(res[id])
		if len(r) > 0 {
			out = append(out, str(r["title"]))
		}
	}
	return out
}
func (s *Service) VerifyArticleType(ctx context.Context, in VerifyArticleTypeInput) Record {
	typ := in.ArticleType
	if typ == "" {
		typ = "case report"
	}
	years := 6
	if in.Years != nil {
		years = *in.Years
	}
	canonical := publicationTypes[strings.ToLower(strings.TrimSpace(typ))]
	if canonical == "" {
		canonical = typ
	}
	q := fmt.Sprintf(`%s[ta] AND "%s"[ptyp]`, in.ISSN, canonical)
	start, current := 0, 0
	if years != 0 {
		current = s.now().Year()
		start = current - years
		q += fmt.Sprintf(" AND %d:%d[dp]", start, current)
	}
	count := s.GetTotalCount(ctx, q)
	r := Record{"total_count": count, "article_type": typ, "years": years, "sample_titles": []string{}, "query": q, "topic_count": nil}
	if count > 0 {
		r["sample_titles"] = s.fetchSampleTitles(ctx, q, 3)
	}
	if in.TopicKeywords != nil && len(*in.TopicKeywords) > 0 && count > 0 {
		parts := []string{}
		for _, x := range *in.TopicKeywords {
			parts = append(parts, fmt.Sprintf(`"%s"[tiab]`, quoteTerm(x)))
		}
		tq := "(" + strings.Join(parts, " OR ") + ") AND " + fmt.Sprintf(`%s[ta] AND "%s"[ptyp]`, in.ISSN, canonical)
		if start != 0 {
			tq += fmt.Sprintf(" AND %d:%d[dp]", start, current)
		}
		r["topic_count"] = s.GetTotalCount(ctx, tq)
		r["topic_query"] = tq
	}
	return r
}
func resolveTypes(p *[]string) []string {
	if p == nil || len(*p) == 0 {
		return append([]string(nil), defaultProfileTypes...)
	}
	out := []string{}
	seen := map[string]bool{}
	for _, x := range *p {
		x = strings.TrimSpace(x)
		if x == "" {
			continue
		}
		c := publicationTypes[strings.ToLower(x)]
		if c == "" {
			c = x
		}
		if !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		return append([]string(nil), defaultProfileTypes...)
	}
	return out
}
func (s *Service) JournalProfile(ctx context.Context, in JournalProfileInput) Record {
	issn := strings.TrimSpace(in.ISSN)
	years := 6
	if in.Years != nil {
		years = *in.Years
	}
	if issn == "" {
		return Record{"error": "issn is required", "issn": in.ISSN, "years": years}
	}
	current := s.now().Year()
	date := ""
	if years != 0 {
		date = fmt.Sprintf(" AND %d:%d[dp]", current-years, current)
	}
	totalQ := issn + "[ta]" + date
	totalN := s.GetTotalCount(ctx, totalQ)
	types := resolveTypes(in.ArticleTypes)
	type result struct {
		i      int
		typ, q string
		n      int
		e      error
	}
	ch := make(chan result, len(types))
	sem := make(chan struct{}, 3)
	var wg sync.WaitGroup
	for i, t := range types {
		wg.Add(1)
		go func(i int, t string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			q := fmt.Sprintf(`%s[ta] AND "%s"[ptyp]%s`, issn, t, date)
			if e := s.sleep(ctx, 350*time.Millisecond); e != nil {
				ch <- result{i, t, q, 0, e}
				return
			}
			n, e := s.GetTotalCountStrict(ctx, q)
			ch <- result{i, t, q, n, e}
		}(i, t)
	}
	wg.Wait()
	close(ch)
	results := make([]result, len(types))
	for r := range ch {
		results[r.i] = r
	}
	counts := map[string]int{}
	queries := []string{}
	errs := []Record{}
	for _, r := range results {
		queries = append(queries, r.q)
		if r.e != nil {
			errs = append(errs, Record{"article_type": r.typ, "error": r.e.Error()})
		} else if r.n > 0 {
			counts[r.typ] = r.n
		}
	}
	sort.Strings(queries)
	article := Record{}
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		return counts[keys[i]] > counts[keys[j]] || counts[keys[i]] == counts[keys[j]] && keys[i] < keys[j]
	})
	for _, k := range keys {
		article[k] = counts[k]
	}
	out := Record{"issn": issn, "years": years, "total_articles": totalN, "article_types": article, "queries": queries}
	if len(errs) > 0 {
		out["errors"] = errs
	}
	return out
}
func (s *Service) JournalMeshProfile(ctx context.Context, in JournalMeshProfileInput) Record {
	issn := strings.TrimSpace(in.ISSN)
	years := 6
	if in.Years != nil {
		years = *in.Years
	}
	if issn == "" {
		return Record{"error": "issn is required", "issn": in.ISSN, "years": years}
	}
	sample := in.SampleSize
	if sample == 0 {
		sample = 100
	}
	if sample < 1 {
		sample = 1
	}
	if sample > 100 {
		sample = 100
	}
	top := in.TopN
	if top == 0 {
		top = 20
	}
	if top < 1 {
		top = 1
	}
	if top > 50 {
		top = 50
	}
	current := s.now().Year()
	date := ""
	if years != 0 {
		date = fmt.Sprintf(" AND %d:%d[dp]", current-years, current)
	}
	q := issn + "[ta]" + date
	ids, totalN, e := s.SearchIDs(ctx, q, sample, "date")
	base := func() Record {
		return Record{"issn": issn, "years": years, "sample_size": sample, "top_n": top, "total_articles": totalN, "sampled_articles": len(ids), "mesh_terms": Record{}, "query": q}
	}
	if e != nil {
		r := base()
		r["total_articles"] = 0
		r["sampled_articles"] = 0
		r["error"] = "PubMed esearch failed or timed out: " + e.Error()
		return r
	}
	if len(ids) == 0 {
		r := base()
		r["pmids"] = []string{}
		return r
	}
	a, e := s.FetchArticles(ctx, ids)
	if e != nil {
		r := base()
		r["pmids"] = ids
		r["error"] = "PubMed efetch failed or timed out: " + e.Error()
		return r
	}
	counts := map[string]int{}
	with := 0
	for _, x := range a {
		seen := map[string]bool{}
		for _, t := range MeshDescriptorNames(x) {
			seen[t] = true
		}
		if len(seen) > 0 {
			with++
		}
		for t := range seen {
			counts[t]++
		}
	}
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		return counts[keys[i]] > counts[keys[j]] || counts[keys[i]] == counts[keys[j]] && keys[i] < keys[j]
	})
	if len(keys) > top {
		keys = keys[:top]
	}
	mesh := Record{}
	for _, k := range keys {
		mesh[k] = counts[k]
	}
	r := base()
	r["articles_with_mesh"] = with
	r["mesh_terms"] = mesh
	r["pmids"] = ids
	return r
}
