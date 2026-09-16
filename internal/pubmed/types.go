package pubmed

import (
	"context"
	"time"
)

const Version = "1.1.0"

type Record map[string]any

type SearchInput struct {
	Query             string    `json:"query"`
	Retmax            *int      `json:"retmax"`
	Title             *[]string `json:"title"`
	Abstract          *[]string `json:"abstract"`
	AllField          *[]string `json:"all_field"`
	Mesh              *[]string `json:"mesh"`
	MeshMajor         *[]string `json:"mesh_major"`
	MeshNoExpand      *[]string `json:"mesh_no_expand"`
	ISSN              *string   `json:"issn"`
	JournalName       *string   `json:"journal_name"`
	Journal           *string   `json:"journal"`
	Author            *[]string `json:"author"`
	YearFrom          *int      `json:"year_from"`
	YearTo            *int      `json:"year_to"`
	ArticleType       *string   `json:"article_type"`
	Language          *string   `json:"language"`
	ProximityTerms    *[]string `json:"proximity_terms"`
	ProximityField    string    `json:"proximity_field"`
	ProximityDistance *int      `json:"proximity_distance"`
	Sort              *string   `json:"sort"`
	FieldJoin         string    `json:"field_join"`
	// ConfirmFull opts in to fetching more than DefaultRetmax (up to
	// HardRetmax) results. It exists so an agent can confirm with its user
	// before pulling a large result set into context.
	ConfirmFull bool `json:"confirm_full,omitempty"`
}

type BatchAbstractInput struct {
	PMIDs            []string `json:"pmids"`
	Delay            float64  `json:"delay"`
	MaxItems         *int     `json:"max_items"`
	MaxAbstractChars int      `json:"max_abstract_chars"`
}

type VerifyArticleTypeInput struct {
	ISSN          string    `json:"issn"`
	ArticleType   string    `json:"article_type"`
	Years         *int      `json:"years"`
	TopicKeywords *[]string `json:"topic_keywords"`
}

type FindRelatedInput struct {
	PMID             string  `json:"pmid"`
	Retmax           *int    `json:"retmax"`
	IncludeAbstracts bool    `json:"include_abstracts"`
	MaxAbstractChars int     `json:"max_abstract_chars"`
	FilterQuery      *string `json:"filter_query"`
}

type ConvertIDsInput struct {
	IDs             []string `json:"ids"`
	IDType          *string  `json:"id_type"`
	IncludeVersions bool     `json:"include_versions"`
}

type FormatCitationsInput struct {
	PMIDs            *[]string `json:"pmids"`
	Papers           *[]Record `json:"papers"`
	Style            string    `json:"style"`
	MaxItems         int       `json:"max_items"`
	IncludeAbstracts bool      `json:"include_abstracts"`
}

type JournalProfileInput struct {
	ISSN         string    `json:"issn"`
	Years        *int      `json:"years"`
	ArticleTypes *[]string `json:"article_types"`
}

type JournalMeshProfileInput struct {
	ISSN       string `json:"issn"`
	Years      *int   `json:"years"`
	SampleSize int    `json:"sample_size"`
	TopN       int    `json:"top_n"`
}

type AbstractSection struct{ Label, Text string }
type RelatedLink struct {
	PMID  string
	Score *int
}

type Options struct {
	HTTP  *HTTPClient
	Now   func() time.Time
	Sleep func(context.Context, time.Duration) error
}

func defaultsSearch(in *SearchInput) {
	if in.Retmax == nil {
		n := 10
		in.Retmax = &n
	}
	if in.ProximityField == "" {
		in.ProximityField = "Title/Abstract"
	}
	if in.ProximityDistance == nil {
		n := 5
		in.ProximityDistance = &n
	}
	if in.FieldJoin == "" {
		in.FieldJoin = "OR"
	}
}
