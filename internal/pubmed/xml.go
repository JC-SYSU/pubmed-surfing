package pubmed

import (
	"encoding/xml"
	"fmt"
	"io"
	"sort"
	"strings"
)

type xmlText struct {
	Inner string `xml:",innerxml"`
	Label string `xml:"Label,attr"`
}
type xmlName struct {
	Text string `xml:",chardata"`
}
type xmlID struct {
	Text string `xml:",chardata"`
	Type string `xml:"EIdType,attr"`
}
type xmlISSN struct {
	Text string `xml:",chardata"`
	Type string `xml:"IssnType,attr"`
}
type xmlAuthor struct {
	Last string `xml:"LastName"`
	Fore string `xml:"ForeName"`
}
type xmlMesh struct {
	Descriptor xmlName   `xml:"DescriptorName"`
	Qualifiers []xmlName `xml:"QualifierName"`
}
type xmlArticle struct {
	PMID          string      `xml:"MedlineCitation>PMID"`
	Title         xmlText     `xml:"MedlineCitation>Article>ArticleTitle"`
	Abstracts     []xmlText   `xml:"MedlineCitation>Article>Abstract>AbstractText"`
	Authors       []xmlAuthor `xml:"MedlineCitation>Article>AuthorList>Author"`
	Journal       string      `xml:"MedlineCitation>Article>Journal>Title"`
	JournalAbbrev string      `xml:"MedlineCitation>Article>Journal>ISOAbbreviation"`
	ISSN          xmlISSN     `xml:"MedlineCitation>Article>Journal>ISSN"`
	Year          string      `xml:"MedlineCitation>Article>Journal>JournalIssue>PubDate>Year"`
	MedlineDate   string      `xml:"MedlineCitation>Article>Journal>JournalIssue>PubDate>MedlineDate"`
	Volume        string      `xml:"MedlineCitation>Article>Journal>JournalIssue>Volume"`
	Issue         string      `xml:"MedlineCitation>Article>Journal>JournalIssue>Issue"`
	Pages         string      `xml:"MedlineCitation>Article>Pagination>MedlinePgn"`
	Locations     []xmlID     `xml:"MedlineCitation>Article>ELocationID"`
	Mesh          []xmlMesh   `xml:"MedlineCitation>MeshHeadingList>MeshHeading"`
}
type xmlSet struct {
	Articles []xmlArticle `xml:"PubmedArticle"`
}

func ParseArticles(data []byte) ([]xmlArticle, error) {
	var set xmlSet
	if err := xml.Unmarshal(data, &set); err != nil {
		return nil, err
	}
	return set.Articles, nil
}
func mixedText(inner string) string {
	dec := xml.NewDecoder(strings.NewReader("<x>" + inner + "</x>"))
	var b strings.Builder
	for {
		tok, e := dec.Token()
		if e == io.EOF {
			break
		}
		if e != nil {
			return compact(inner)
		}
		if c, ok := tok.(xml.CharData); ok {
			b.Write([]byte(c))
		}
	}
	return compact(b.String())
}
func yearOf(a xmlArticle) string {
	if y := compact(a.Year); y != "" {
		return y
	}
	for _, f := range strings.Fields(a.MedlineDate) {
		if len(f) >= 4 {
			for i := 0; i+4 <= len(f); i++ {
				s := f[i : i+4]
				if s[0] >= '1' && s[0] <= '2' && s[1] >= '0' && s[1] <= '9' && s[2] >= '0' && s[2] <= '9' && s[3] >= '0' && s[3] <= '9' {
					return s
				}
			}
		}
	}
	return ""
}
func articleRecord(a xmlArticle, fallback string) Record {
	pmid := compact(a.PMID)
	if pmid == "" {
		pmid = fallback
	}
	sections := []Record{}
	parts := []string{}
	for _, s := range a.Abstracts {
		text := mixedText(s.Inner)
		label := compact(s.Label)
		if label != "" {
			parts = append(parts, label+": "+text)
		} else {
			parts = append(parts, text)
		}
		if text != "" {
			sections = append(sections, Record{"label": label, "text": text})
		}
	}
	authors := []string{}
	for _, x := range a.Authors {
		n := strings.TrimSpace(compact(x.Last) + " " + compact(x.Fore))
		if n != "" {
			authors = append(authors, n)
		}
		if len(authors) == 10 {
			break
		}
	}
	doi := ""
	for _, x := range a.Locations {
		if x.Type == "doi" {
			doi = x.Text
			break
		}
	}
	mesh := []string{}
	for _, m := range a.Mesh {
		d := compact(m.Descriptor.Text)
		if d == "" {
			continue
		}
		q := []string{}
		for _, x := range m.Qualifiers {
			if t := compact(x.Text); t != "" {
				q = append(q, t)
			}
		}
		if len(q) > 0 {
			d += "/" + strings.Join(q, "/")
		}
		mesh = append(mesh, d)
	}
	return Record{"pmid": pmid, "title": mixedText(a.Title.Inner), "abstract": strings.Join(parts, "\n"), "abstract_sections": sections, "authors": strings.Join(authors, "; "), "journal": a.Journal, "journal_abbrev": a.JournalAbbrev, "year": yearOf(a), "volume": a.Volume, "issue": a.Issue, "pages": a.Pages, "doi": doi, "mesh_terms": strings.Join(mesh, "; ")}
}
func MeshDescriptorNames(a xmlArticle) []string {
	out := []string{}
	for _, m := range a.Mesh {
		if s := compact(m.Descriptor.Text); s != "" {
			out = append(out, s)
		}
	}
	return out
}
func ExtractJournalISSNs(a xmlArticle) (string, string) {
	raw := compact(a.ISSN.Text)
	if strings.EqualFold(a.ISSN.Type, "Electronic") {
		return "", raw
	}
	return raw, ""
}

func TruncateAbstractItem(item Record, requested int) Record {
	raw, _ := item["abstract"].(string)
	limit := requested
	if limit == 0 {
		limit = 1800
	}
	if limit < 500 {
		limit = 500
	}
	if limit > 5000 {
		limit = 5000
	}
	if len(raw) <= limit {
		return item
	}
	out := cloneRecord(item)
	sections := []AbstractSection{}
	if arr, ok := item["abstract_sections"].([]Record); ok {
		for _, r := range arr {
			sections = append(sections, AbstractSection{fmt.Sprint(r["label"]), fmt.Sprint(r["text"])})
		}
	}
	if len(sections) > 0 {
		out["abstract"] = TruncateAbstractSections(sections, limit)
		out["abstract_truncation_strategy"] = "section-aware"
	} else {
		suffix := fmt.Sprintf("\n... (truncated at %d chars)", limit)
		n := limit - len(suffix)
		if n < 0 {
			n = 0
		}
		out["abstract"] = strings.TrimRight(raw[:n], " ") + suffix
		out["abstract_truncation_strategy"] = "character"
	}
	out["abstract_truncated"] = "true"
	out["abstract_length"] = fmt.Sprint(len(raw))
	return out
}
func sectionPriority(label string) int {
	switch strings.ToUpper(compact(label)) {
	case "BACKGROUND", "OBJECTIVE", "OBJECTIVES", "PURPOSE", "AIM", "AIMS":
		return 0
	case "CONCLUSION", "CONCLUSIONS", "INTERPRETATION":
		return 1
	case "RESULT", "RESULTS", "FINDINGS":
		return 2
	case "METHOD", "METHODS", "MATERIALS AND METHODS", "METHODS AND RESULTS":
		return 4
	}
	return 3
}
func TruncateAbstractSections(sections []AbstractSection, limit int) string {
	type row struct {
		i, p int
		s    string
	}
	rows := []row{}
	complete := []string{}
	for i, x := range sections {
		s := compact(x.Text)
		if x.Label != "" {
			s = compact(x.Label) + ": " + s
		}
		if s != "" {
			rows = append(rows, row{i, sectionPriority(x.Label), s})
			complete = append(complete, s)
		}
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].p < rows[j].p || rows[i].p == rows[j].p && rows[i].i < rows[j].i })
	suffix := "\n... (section-aware truncated)"
	used := 0
	chosen := []row{}
	trunc := false
	for _, r := range rows {
		sep := 0
		if len(chosen) > 0 {
			sep = 1
		}
		if used+sep+len(r.s) <= limit-len(suffix) {
			chosen = append(chosen, r)
			used += sep + len(r.s)
			continue
		}
		remain := limit - len(suffix) - used - sep
		if remain >= 80 {
			r.s = strings.TrimRight(r.s[:remain], " ")
			chosen = append(chosen, r)
		}
		trunc = true
		break
	}
	if len(chosen) == 0 && len(rows) > 0 {
		r := rows[0]
		n := limit - len(suffix)
		if n < 0 {
			n = 0
		}
		if len(r.s) > n {
			r.s = r.s[:n]
		}
		chosen = append(chosen, r)
		trunc = true
	}
	sort.Slice(chosen, func(i, j int) bool { return chosen[i].i < chosen[j].i })
	ss := []string{}
	for _, r := range chosen {
		ss = append(ss, r.s)
	}
	result := strings.Join(ss, "\n")
	if trunc || len(result) < len(strings.Join(complete, "\n")) {
		result = strings.TrimRight(result, " ") + suffix
	}
	if len(result) > limit {
		result = result[:limit]
	}
	return strings.TrimRight(result, " ")
}
func cloneRecord(r Record) Record {
	n := Record{}
	for k, v := range r {
		n[k] = v
	}
	return n
}
