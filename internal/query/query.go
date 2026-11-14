// Package query builds the Bleve search request from user input
// (positional terms + repeatable --field filters) and renders results.
package query

import (
	"sort"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search/query"

	// Register the ANSI highlighter so NewHighlightWithStyle("ansi") works.
	_ "github.com/blevesearch/bleve/v2/search/highlight/highlighter/ansi"

	"github.com/mjpitz/codesearch/internal/index"
)

// Request describes a single search.
type Request struct {
	Terms     string             // positional query string; empty -> match all
	Fields    map[string]string  // exact-match filters AND'd with the query
	Limit     int                // 0 -> 10
	NoSnippet bool               // disable highlight extraction
	Boosts    map[string]float64 // field -> boost (only applied when > 0)
	Highlight string             // "ansi", "html", or "" (empty disables)
	// VectorRanking is an optional pre-computed path→rank map (1-indexed,
	// 1 = most similar) from a vector search. When non-nil, Search merges
	// it with BM25 results using Reciprocal Rank Fusion (k=60).
	VectorRanking map[string]int
}

// Hit is one ranked search result.
type Hit struct {
	Path    string              `json:"path"`
	Score   float64             `json:"score"`
	Title   string              `json:"title,omitempty"`
	Tags    []string            `json:"tags,omitempty"`
	Snippet string              `json:"snippet,omitempty"`
	Extras  map[string]any      `json:"extras,omitempty"`
	Frags   map[string][]string `json:"fragments,omitempty"`
}

// Result is the response shape returned to the CLI and MCP layers.
type Result struct {
	Total uint64 `json:"total"`
	Hits  []Hit  `json:"hits"`
}

// defaultBoostedFields lists fields that the query layer will spread the
// user terms across, as a fallback when Request.Boosts is nil/empty. The
// CLI passes the configured boosts down so this default is rarely used.
var defaultBoostedFields = map[string]float64{
	"title": 5.0,
	"tags":  2.0,
	"body":  1.0,
}

// Search runs the request against idx and returns ranked hits. When
// req.VectorRanking is non-nil, results are merged with BM25 via RRF (k=60).
func Search(idx *index.Index, req Request) (*Result, error) {
	if req.Limit <= 0 {
		req.Limit = 10
	}

	textResult, err := textSearch(idx, req)
	if err != nil {
		return nil, err
	}

	if req.VectorRanking == nil {
		return textResult, nil
	}

	return mergeRRF(textResult, req.VectorRanking, req.Limit), nil
}

// textSearch runs the BM25 query and returns ranked hits.
func textSearch(idx *index.Index, req Request) (*Result, error) {
	boosts := req.Boosts
	if len(boosts) == 0 {
		boosts = defaultBoostedFields
	}

	var q query.Query
	if req.Terms == "" {
		q = bleve.NewMatchAllQuery()
	} else {
		q = buildTermsQuery(req.Terms, boosts)
	}

	if len(req.Fields) > 0 {
		conj := bleve.NewConjunctionQuery(q)
		for field, value := range req.Fields {
			tq := bleve.NewTermQuery(value)
			tq.SetField(field)
			conj.AddQuery(tq)
		}
		q = conj
	}

	bleveReq := bleve.NewSearchRequestOptions(q, req.Limit, 0, false)
	bleveReq.Fields = []string{"path", "title", "tags", "description"}

	if !req.NoSnippet && req.Highlight != "" {
		bleveReq.Highlight = bleve.NewHighlightWithStyle(req.Highlight)
		bleveReq.Highlight.Fields = []string{"body", "title", "description"}
	}

	sr, err := idx.Bleve().Search(bleveReq)
	if err != nil {
		return nil, err
	}

	out := &Result{Total: sr.Total}
	for _, h := range sr.Hits {
		hit := Hit{
			Path:  h.ID,
			Score: h.Score,
			Frags: h.Fragments,
		}

		if v, ok := h.Fields["title"].(string); ok {
			hit.Title = v
		}

		switch tags := h.Fields["tags"].(type) {
		case []any:
			for _, t := range tags {
				if s, ok := t.(string); ok {
					hit.Tags = append(hit.Tags, s)
				}
			}
		case []string:
			hit.Tags = tags
		case string:
			hit.Tags = []string{tags}
		}

		hit.Snippet = firstFragment(h.Fragments, "body", "description", "title")

		out.Hits = append(out.Hits, hit)
	}

	return out, nil
}

// mergeRRF combines text search results with a pre-computed vector ranking
// using Reciprocal Rank Fusion (k=60). Documents in only one list get a
// fallback rank of len(thatList)+1.
func mergeRRF(textResult *Result, vectorRanking map[string]int, limit int) *Result {
	const k = 60

	textRanking := make(map[string]int, len(textResult.Hits))
	for i, h := range textResult.Hits {
		textRanking[h.Path] = i + 1
	}

	seen := make(map[string]bool, len(textRanking)+len(vectorRanking))
	for p := range textRanking {
		seen[p] = true
	}
	for p := range vectorRanking {
		seen[p] = true
	}

	textFallback := len(textRanking) + 1
	vecFallback := len(vectorRanking) + 1

	type scored struct {
		path  string
		score float64
	}

	merged := make([]scored, 0, len(seen))
	for p := range seen {
		tr, vr := textRanking[p], vectorRanking[p]
		if tr == 0 {
			tr = textFallback
		}
		if vr == 0 {
			vr = vecFallback
		}
		merged = append(merged, scored{p, 1/float64(k+tr) + 1/float64(k+vr)})
	}

	sort.Slice(merged, func(i, j int) bool { return merged[i].score > merged[j].score })

	hitByPath := make(map[string]Hit, len(textResult.Hits))
	for _, h := range textResult.Hits {
		hitByPath[h.Path] = h
	}

	if limit > len(merged) {
		limit = len(merged)
	}

	out := &Result{Total: uint64(len(seen))}
	for _, s := range merged[:limit] {
		if h, ok := hitByPath[s.path]; ok {
			h.Score = s.score
			out.Hits = append(out.Hits, h)
		} else {
			out.Hits = append(out.Hits, Hit{Path: s.path, Score: s.score})
		}
	}

	return out
}

func buildTermsQuery(terms string, boosts map[string]float64) query.Query {
	dq := bleve.NewDisjunctionQuery()
	for field, boost := range boosts {
		if boost <= 0 {
			continue
		}
		mq := bleve.NewMatchQuery(terms)
		mq.SetField(field)
		mq.SetBoost(boost)
		dq.AddQuery(mq)
	}

	return dq
}

func firstFragment(frags map[string][]string, fields ...string) string {
	for _, f := range fields {
		if list, ok := frags[f]; ok && len(list) > 0 {
			return list[0]
		}
	}

	return ""
}
