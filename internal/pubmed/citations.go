package pubmed

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

type citationRecord struct {
	PMID, Title, Journal, JournalAbbrev, Year, Volume, Issue, Pages, DOI, URL, Abstract string
	Authors                                                                             []string
}

func citationAuthors(v any) []string {
	out := []string{}
	switch x := v.(type) {
	case []string:
		for _, s := range x {
			if s = compact(s); s != "" {
				out = append(out, s)
			}
		}
	case []any:
		for _, a := range x {
			if s := compact(a); s != "" {
				out = append(out, s)
			}
		}
	default:
		for _, s := range strings.Split(fmt.Sprint(v), ";") {
			if s = compact(s); s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}
func normalizeCitation(r Record) citationRecord {
	c := citationRecord{PMID: compact(r["pmid"]), Title: compact(r["title"]), Authors: citationAuthors(r["authors"]), Journal: compact(r["journal"]), JournalAbbrev: compact(r["journal_abbrev"]), Year: compact(r["year"]), Volume: compact(r["volume"]), Issue: compact(r["issue"]), Pages: compact(r["pages"]), DOI: compact(r["doi"]), URL: compact(r["url"]), Abstract: compact(r["abstract"])}
	if c.Year == "" {
		c.Year = "n.d."
	}
	if c.URL == "" {
		if c.DOI != "" {
			c.URL = "https://doi.org/" + c.DOI
		} else if c.PMID != "" {
			c.URL = "https://pubmed.ncbi.nlm.nih.gov/" + c.PMID + "/"
		}
	}
	return c
}
func splitAuthor(a string) (string, string) {
	a = compact(a)
	if strings.Contains(a, ",") {
		p := strings.Split(a, ",")
		return compact(p[0]), compact(strings.Join(p[1:], ","))
	}
	p := strings.Fields(a)
	if len(p) == 0 {
		return "", ""
	}
	if len(p) == 1 {
		return p[0], ""
	}
	return p[0], strings.Join(p[1:], " ")
}
func apaAuthor(a string) string {
	last, first := splitAuthor(a)
	ini := []string{}
	for _, p := range strings.Fields(strings.ReplaceAll(first, "-", " ")) {
		r, _ := utf8.DecodeRuneInString(p)
		ini = append(ini, string(r)+".")
	}
	return strings.TrimSpace(last + ", " + strings.Join(ini, " "))
}
func mlaAuthor(a string, firstOne bool) string {
	last, first := splitAuthor(a)
	if firstOne {
		return strings.TrimSuffix(strings.TrimSpace(last+", "+first), ",")
	}
	return strings.TrimSpace(first + " " + last)
}
func authorText(a []string, style string) string {
	if len(a) == 0 {
		return ""
	}
	if style == "apa" {
		f := []string{}
		for _, x := range a {
			f = append(f, apaAuthor(x))
		}
		if len(f) == 1 {
			return f[0]
		}
		if len(f) == 2 {
			return strings.Join(f, " & ")
		}
		return strings.Join(f[:len(f)-1], ", ") + ", & " + f[len(f)-1]
	}
	if style == "mla" {
		if len(a) == 1 {
			return mlaAuthor(a[0], true)
		}
		if len(a) == 2 {
			return mlaAuthor(a[0], true) + ", and " + mlaAuthor(a[1], false)
		}
		return mlaAuthor(a[0], true) + ", et al."
	}
	return strings.Join(a, "; ")
}
func FormatCitation(c citationRecord, style string, used map[string]int) string {
	switch style {
	case "apa":
		a := authorText(c.Authors, "apa")
		if a == "" {
			a = "Unknown"
		}
		s := fmt.Sprintf("%s (%s). %s.", a, c.Year, c.Title)
		if c.Journal != "" {
			s += " " + c.Journal
			if c.Volume != "" {
				s += ", " + c.Volume
				if c.Issue != "" {
					s += "(" + c.Issue + ")"
				}
			}
			if c.Pages != "" {
				s += ", " + c.Pages
			}
			s += "."
		}
		if c.DOI != "" {
			s += " https://doi.org/" + c.DOI
		} else if c.URL != "" {
			s += " " + c.URL
		}
		return s
	case "mla":
		a := authorText(c.Authors, "mla")
		if a == "" {
			a = "Unknown"
		}
		s := a + `. "` + c.Title + `."`
		if c.Journal != "" {
			s += " " + c.Journal
			if c.Volume != "" {
				s += ", vol. " + c.Volume
			}
			if c.Issue != "" {
				s += ", no. " + c.Issue
			}
			if c.Year != "n.d." {
				s += ", " + c.Year
			}
			if c.Pages != "" {
				s += ", pp. " + c.Pages
			}
			s += "."
		}
		if c.DOI != "" {
			s += " doi:" + c.DOI + "."
		} else if c.URL != "" {
			s += " " + c.URL + "."
		}
		return s
	case "bibtex":
		base := "article" + fmt.Sprint(len(used)+1)
		if c.PMID != "" {
			base = "pmid" + c.PMID
		}
		n := used[base]
		used[base] = n + 1
		key := base
		if n > 0 {
			key += string(rune('a' + n - 1))
		}
		lines := []string{"@article{" + key + ","}
		add := func(k, v string) {
			if v != "" {
				if k == "abstract" {
					lines = append(lines, fmt.Sprintf("  abstract = {%s},", v))
				} else {
					lines = append(lines, fmt.Sprintf("  %-8s= {%s},", k, v))
				}
			}
		}
		add("author", strings.Join(c.Authors, " and "))
		add("title", "{"+strings.ReplaceAll(c.Title, "&", `\&`)+"}")
		add("journal", strings.ReplaceAll(c.Journal, "&", `\&`))
		if c.Year != "n.d." {
			add("year", c.Year)
		}
		add("volume", c.Volume)
		add("number", c.Issue)
		add("pages", strings.ReplaceAll(c.Pages, "-", "--"))
		add("doi", c.DOI)
		add("url", c.URL)
		add("abstract", strings.ReplaceAll(c.Abstract, "&", `\&`))
		add("pmid", c.PMID)
		if len(lines) > 1 {
			lines[len(lines)-1] = strings.TrimSuffix(lines[len(lines)-1], ",")
		}
		return strings.Join(append(lines, "}"), "\n")
	case "ris":
		lines := []string{"TY  - JOUR"}
		for _, a := range c.Authors {
			lines = append(lines, "AU  - "+compact(a))
		}
		add := func(k, v string) {
			if v != "" {
				lines = append(lines, k+"  - "+v)
			}
		}
		add("TI", c.Title)
		if c.Journal != "" {
			add("JO", c.Journal)
			add("T2", c.Journal)
		}
		add("JA", c.JournalAbbrev)
		if c.Year != "n.d." {
			add("PY", c.Year)
		}
		add("VL", c.Volume)
		add("IS", c.Issue)
		if c.Pages != "" {
			p := strings.SplitN(c.Pages, "-", 2)
			add("SP", strings.TrimSpace(p[0]))
			if len(p) > 1 {
				add("EP", strings.TrimSpace(p[1]))
			}
		}
		add("DO", c.DOI)
		add("UR", c.URL)
		if c.Abstract != "" {
			a := c.Abstract
			if len(a) > 500 {
				a = a[:500]
			}
			add("N2", a)
		}
		if c.PMID != "" {
			add("AN", "PMID:"+c.PMID)
		}
		return strings.Join(append(lines, "DB  - PubMed", "ER  - "), "\n")
	}
	return ""
}
