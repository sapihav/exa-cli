// Package client wraps the Exa AI HTTP API.
//
// M1 scope: /search endpoint only. No contents, no find-similar, no answer.
package client

// SearchRequest is the JSON body sent to POST /search.
//
// Only the three fields required by Milestone 1 are modelled. When future
// milestones need more (contents, livecrawl, date filters) add them here and
// keep them `omitempty` so they do not appear in existing request bodies.
type SearchRequest struct {
	Query      string `json:"query"`
	NumResults int    `json:"numResults,omitempty"`
	Type       string `json:"type,omitempty"`
}

// SearchResult is one item in the `results` array of a /search response.
//
// Fields mirror the documented schema at https://docs.exa.ai/reference/search.
// All are nullable upstream; we use pointers only where the zero value is
// meaningful (Score=0 is distinct from absent), otherwise the empty string
// is good enough for downstream consumers.
type SearchResult struct {
	Title         string   `json:"title,omitempty"`
	URL           string   `json:"url,omitempty"`
	ID            string   `json:"id,omitempty"`
	PublishedDate string   `json:"publishedDate,omitempty"`
	Author        string   `json:"author,omitempty"`
	Score         *float64 `json:"score,omitempty"`
	Image         string   `json:"image,omitempty"`
	Favicon       string   `json:"favicon,omitempty"`
}

// SearchResponse is the full /search response payload.
//
// `AutopromptString` is present when Exa rewrote the query under the hood
// (common for `type: auto`); we surface it so callers can audit what was
// actually searched for.
type SearchResponse struct {
	RequestID        string         `json:"requestId,omitempty"`
	ResolvedSearch   string         `json:"resolvedSearchType,omitempty"`
	AutopromptString string         `json:"autopromptString,omitempty"`
	Results          []SearchResult `json:"results"`
}
