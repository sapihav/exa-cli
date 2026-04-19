// Package client wraps the Exa AI HTTP API.
//
// M1 scope: /search endpoint. M2 adds /contents.
package client

import "encoding/json"

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

// ContentsRequest is the JSON body sent to POST /contents.
//
// Upstream accepts rich sub-objects for text/summary/highlights (custom
// queries, schemas, section filters). M2 models the common-case primitives:
// a boolean toggle for text, a primitive count for highlights/subpages, and
// a boolean for summary. If callers later need custom queries/schemas we
// can widen these to object types without breaking the JSON contract
// (the field names stay the same).
//
// See https://docs.exa.ai/reference/contents-retrieval-with-exa-api.
type ContentsRequest struct {
	URLs       []string `json:"urls"`
	Text       bool     `json:"text,omitempty"`
	Summary    bool     `json:"summary,omitempty"`
	Highlights int      `json:"highlights,omitempty"`
	Subpages   int      `json:"subpages,omitempty"`
	Livecrawl  string   `json:"livecrawl,omitempty"`
}

// ContentsResult is one item in the `results` array of a /contents response.
//
// The fields are a superset of SearchResult — anything the caller asked for
// (text/summary/highlights/subpages) is present, the rest are omitted.
type ContentsResult struct {
	ID            string           `json:"id,omitempty"`
	URL           string           `json:"url,omitempty"`
	Title         string           `json:"title,omitempty"`
	Author        string           `json:"author,omitempty"`
	PublishedDate string           `json:"publishedDate,omitempty"`
	Text          string           `json:"text,omitempty"`
	Summary       string           `json:"summary,omitempty"`
	Highlights    []string         `json:"highlights,omitempty"`
	Subpages      []ContentsResult `json:"subpages,omitempty"`
	Image         string           `json:"image,omitempty"`
	Favicon       string           `json:"favicon,omitempty"`
}

// ContentsStatus reports per-URL outcome. Exa reports per-URL crawl failures
// here rather than failing the whole request, so surfacing it lets callers
// see which URLs were dropped and why without parsing error strings.
//
// `HTTPStatusCode` is json.RawMessage because upstream returns it as either a
// JSON number (e.g., 404) or — for non-HTTP failures — an object or string.
// Decoding into a concrete type would fail the whole response parse on any
// variant; RawMessage passes it through verbatim for callers to inspect.
type ContentsStatus struct {
	ID     string `json:"id,omitempty"`
	Status string `json:"status,omitempty"`
	Error  *struct {
		Tag            string          `json:"tag,omitempty"`
		HTTPStatusCode json.RawMessage `json:"httpStatusCode,omitempty"`
	} `json:"error,omitempty"`
}

// ContentsResponse is the full /contents response payload.
type ContentsResponse struct {
	RequestID string           `json:"requestId,omitempty"`
	Results   []ContentsResult `json:"results"`
	Statuses  []ContentsStatus `json:"statuses,omitempty"`
}
