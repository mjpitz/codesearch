// Package mcp serves the codesearch tools over MCP via stdio.
package mcp

// SearchArgs is the typed input for codesearch_search.
type SearchArgs struct {
	Query  string            `json:"query"`
	Fields map[string]string `json:"fields,omitempty"`
	Limit  int               `json:"limit,omitempty"`
}

// FacetsArgs is the typed input for codesearch_facets.
type FacetsArgs struct {
	Field string `json:"field"`
	Limit int    `json:"limit,omitempty"`
}

// GetArgs is the typed input for codesearch_get.
type GetArgs struct {
	Path     string `json:"path"`
	MaxBytes int    `json:"max_bytes,omitempty"`
}

// FacetResult is one term entry returned by codesearch_facets.
type FacetResult struct {
	Term  string `json:"term"`
	Count int    `json:"count"`
}

// GetResult is the response from codesearch_get.
type GetResult struct {
	Path      string `json:"path"`
	Content   string `json:"content"`
	Truncated bool   `json:"truncated"`
}

// StatusResult is the response from codesearch_status.
type StatusResult struct {
	IndexPath      string `json:"index_path"`
	DocCount       uint64 `json:"doc_count"`
	IndexSizeBytes int64  `json:"index_size_bytes"`
	SchemaVersion  int    `json:"schema_version"`
	LastSyncAt     string `json:"last_sync_at"`
}
