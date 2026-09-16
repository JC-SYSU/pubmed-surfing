package pubmed

import (
	"fmt"
	"strings"
)

// Result-count bounds. These are LAYER 1 of the size limits: how many papers
// one search returns to the AI client. They have nothing to do with how many
// PMIDs fit into a single HTTP request (see EsummaryPostThreshold in
// http.go for that separate, transport-layer concern).
//
// DefaultRetmax is a product decision, not an NCBI limit: it caps the MCP
// response size (~100 records keeps context use bounded) and doubles as the
// confirmation threshold — a search wanting more than this returns
// truncated=true and the agent must ask its user before retrying with
// SearchInput.ConfirmFull. NCBI's own retmax bounds are looser: default 20,
// maximum 10,000 (https://www.ncbi.nlm.nih.gov/books/NBK25499/).
const DefaultRetmax = 100

// HardRetmax mirrors the NCBI ESearch limit for PubMed: a search can only
// retrieve the first 10,000 matching records. It bounds ConfirmFull fetches.
const HardRetmax = 10000

func CoerceRetmax(n int) int {
	if n < 0 {
		return 0
	}
	if n > DefaultRetmax {
		return DefaultRetmax
	}
	return n
}
func compact(v any) string {
	if v == nil {
		return ""
	}
	return strings.Join(strings.Fields(fmt.Sprint(v)), " ")
}
func quoteTerm(v string) string { return strings.ReplaceAll(v, `"`, `\"`) }

func addTerms(parts *[]string, values *[]string, field, join string) {
	if values == nil {
		return
	}
	clean := []string{}
	for _, v := range *values {
		if v = strings.TrimSpace(v); v != "" {
			clean = append(clean, fmt.Sprintf(`"%s"[%s]`, quoteTerm(v), field))
		}
	}
	if len(clean) > 0 {
		*parts = append(*parts, "("+strings.Join(clean, " "+join+" ")+")")
	}
}

func BuildPubmedQuery(in SearchInput) string {
	join := strings.ToUpper(strings.TrimSpace(in.FieldJoin))
	if join != "AND" {
		join = "OR"
	}
	parts := []string{}
	addTerms(&parts, in.Title, "Title", join)
	addTerms(&parts, in.Abstract, "Title/Abstract", join)
	addTerms(&parts, in.AllField, "All Fields", join)
	addTerms(&parts, in.Mesh, "mh", join)
	addTerms(&parts, in.MeshMajor, "majr", join)
	addTerms(&parts, in.MeshNoExpand, "mh:noexp", join)
	if in.ISSN != nil && strings.TrimSpace(*in.ISSN) != "" {
		parts = append(parts, fmt.Sprintf(`%s[ta]`, strings.TrimSpace(*in.ISSN)))
	}
	j := in.Journal
	if (j == nil || *j == "") && in.JournalName != nil {
		j = in.JournalName
	}
	if j != nil && strings.TrimSpace(*j) != "" {
		parts = append(parts, fmt.Sprintf(`"%s"[ta]`, quoteTerm(strings.TrimSpace(*j))))
	}
	addTerms(&parts, in.Author, "au", join)
	if in.YearFrom != nil && in.YearTo != nil {
		parts = append(parts, fmt.Sprintf("(%d[dp] : %d[dp])", *in.YearFrom, *in.YearTo))
	} else if in.YearFrom != nil {
		parts = append(parts, fmt.Sprintf("%d[dp]", *in.YearFrom))
	} else if in.YearTo != nil {
		parts = append(parts, fmt.Sprintf("%d[dp]", *in.YearTo))
	}
	if in.ArticleType != nil && strings.TrimSpace(*in.ArticleType) != "" {
		parts = append(parts, fmt.Sprintf(`"%s"[ptyp]`, quoteTerm(strings.TrimSpace(*in.ArticleType))))
	}
	if in.Language != nil && strings.TrimSpace(*in.Language) != "" {
		parts = append(parts, fmt.Sprintf(`%s[la]`, strings.TrimSpace(*in.Language)))
	}
	return strings.Join(parts, " AND ")
}

func BuildPubmedProximityQuery(terms []string, field string, distance int) string {
	clean := []string{}
	for _, t := range terms {
		if t = strings.TrimSpace(t); t != "" {
			clean = append(clean, quoteTerm(t))
		}
	}
	if len(clean) == 0 {
		return ""
	}
	aliases := map[string]string{"ti": "Title", "tiab": "Title/Abstract", "ad": "Affiliation"}
	if x := aliases[field]; x != "" {
		field = x
	}
	if field == "" {
		field = "Title/Abstract"
	}
	return fmt.Sprintf(`"%s"[%s:~%d]`, strings.Join(clean, " "), field, distance)
}
