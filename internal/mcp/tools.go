package mcp

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/blevesearch/bleve/v2"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/mjpitz/codesearch/internal/config"
	"github.com/mjpitz/codesearch/internal/index"
	"github.com/mjpitz/codesearch/internal/query"
	"github.com/mjpitz/codesearch/internal/sync"
)

const (
	getDefaultMaxBytes = 64 * 1024
	getHardMaxBytes    = 256 * 1024
	facetDefaultLimit  = 50
)

// registerTools attaches the codesearch_* MCP tools to srv. Each tool
// that touches the index acquires the shared handle from lazy on entry
// and releases it on return, so the OS file lock is released between
// bursts of requests.
func registerTools(srv *server.MCPServer, cfg *config.IndexConfig, lazy *index.LazyIndex) {
	srv.AddTool(searchTool(), mcp.NewStructuredToolHandler(searchHandler(cfg, lazy)))
	srv.AddTool(facetsTool(), mcp.NewStructuredToolHandler(facetsHandler(lazy)))
	srv.AddTool(fieldsTool(), fieldsHandler(lazy))
	srv.AddTool(getTool(), mcp.NewStructuredToolHandler(getHandler(cfg)))
	srv.AddTool(statusTool(), statusHandler(cfg, lazy))
}

// --- search ---

func searchTool() mcp.Tool {
	return mcp.NewTool("codesearch_search",
		mcp.WithDescription("Ranked full-text search across indexed docs and configs. Returns ranked hits with path, score, title, snippet."),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithString("query",
			mcp.Required(),
			mcp.Description("Search terms. Empty matches all docs."),
		),
		mcp.WithObject("fields",
			mcp.Description("Exact-match field filters AND'd with the query, e.g. {\"tags\":\"research\"}."),
			mcp.AdditionalProperties(map[string]any{"type": "string"}),
		),
		mcp.WithNumber("limit",
			mcp.Description("Max hits to return (default 10)."),
			mcp.Min(1),
			mcp.Max(100),
		),
	)
}

func searchHandler(cfg *config.IndexConfig, lazy *index.LazyIndex) func(context.Context, mcp.CallToolRequest, SearchArgs) (*query.Result, error) {
	return func(ctx context.Context, req mcp.CallToolRequest, args SearchArgs) (*query.Result, error) {
		idx, release, err := lazy.Acquire()
		if err != nil {
			return nil, err
		}
		defer release()

		r := query.Request{
			Terms:     args.Query,
			Fields:    args.Fields,
			Limit:     args.Limit,
			Boosts:    cfg.Boosts,
			Highlight: "html",
		}
		return query.Search(idx, r)
	}
}

// --- facets ---

func facetsTool() mcp.Tool {
	return mcp.NewTool("codesearch_facets",
		mcp.WithDescription("Return the distinct values seen in a given indexed field, with document counts. Use codesearch_fields to discover field names."),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithString("field",
			mcp.Required(),
			mcp.Description("Field name to facet on (e.g. tags, service.name, doc_type)."),
		),
		mcp.WithNumber("limit",
			mcp.Description("Max distinct values to return (default 50)."),
			mcp.Min(1),
			mcp.Max(500),
		),
	)
}

func facetsHandler(lazy *index.LazyIndex) func(context.Context, mcp.CallToolRequest, FacetsArgs) ([]FacetResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest, args FacetsArgs) ([]FacetResult, error) {
		if strings.TrimSpace(args.Field) == "" {
			return nil, errors.New("field is required")
		}
		limit := args.Limit
		if limit <= 0 {
			limit = facetDefaultLimit
		}

		idx, release, err := lazy.Acquire()
		if err != nil {
			return nil, err
		}
		defer release()

		bleveReq := bleve.NewSearchRequestOptions(bleve.NewMatchAllQuery(), 0, 0, false)
		bleveReq.AddFacet(args.Field, bleve.NewFacetRequest(args.Field, limit))

		result, err := idx.Bleve().Search(bleveReq)
		if err != nil {
			return nil, err
		}

		out := []FacetResult{}
		facet, ok := result.Facets[args.Field]
		if !ok || facet == nil {
			return out, nil
		}
		for _, t := range facet.Terms.Terms() {
			out = append(out, FacetResult{Term: t.Term, Count: t.Count})
		}
		return out, nil
	}
}

// --- fields ---

func fieldsTool() mcp.Tool {
	return mcp.NewTool("codesearch_fields",
		mcp.WithDescription("List every indexed field name in the codesearch index. Use the returned names with codesearch_facets or as keys in codesearch_search's fields filter."),
		mcp.WithDestructiveHintAnnotation(false),
	)
}

func fieldsHandler(lazy *index.LazyIndex) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		idx, release, err := lazy.Acquire()
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		defer release()

		fields, err := idx.Bleve().Fields()
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultJSON(fields)
	}
}

// --- get ---

func getTool() mcp.Tool {
	return mcp.NewTool("codesearch_get",
		mcp.WithDescription("Return the contents of a repo-relative path. Refuses absolute paths and `..` escapes. Truncates beyond max_bytes (default 65536, hard cap 262144)."),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithString("path",
			mcp.Required(),
			mcp.Description("Repo-relative path (forward slashes)."),
		),
		mcp.WithNumber("max_bytes",
			mcp.Description("Maximum bytes to return (default 65536, max 262144)."),
			mcp.Min(1),
			mcp.Max(getHardMaxBytes),
		),
	)
}

func getHandler(cfg *config.IndexConfig) func(context.Context, mcp.CallToolRequest, GetArgs) (*GetResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest, args GetArgs) (*GetResult, error) {
		abs, err := resolveRepoPath(cfg.Root, args.Path)
		if err != nil {
			return nil, err
		}

		maxBytes := args.MaxBytes
		if maxBytes <= 0 {
			maxBytes = getDefaultMaxBytes
		}
		if maxBytes > getHardMaxBytes {
			maxBytes = getHardMaxBytes
		}

		body, err := os.ReadFile(abs)
		if err != nil {
			return nil, err
		}

		truncated := false
		if len(body) > maxBytes {
			body = body[:maxBytes]
			truncated = true
		}

		return &GetResult{Path: args.Path, Content: string(body), Truncated: truncated}, nil
	}
}

func resolveRepoPath(root, rel string) (string, error) {
	if rel == "" {
		return "", errors.New("path is required")
	}
	if filepath.IsAbs(rel) {
		return "", errors.New("absolute paths are not allowed")
	}
	clean := filepath.Clean(filepath.FromSlash(rel))
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", errors.New("path escapes the repo root")
	}
	abs := filepath.Join(root, clean)
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	absAbs, err := filepath.Abs(abs)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(absAbs, rootAbs+string(filepath.Separator)) && absAbs != rootAbs {
		return "", errors.New("path escapes the repo root")
	}
	return absAbs, nil
}

// --- status ---

func statusTool() mcp.Tool {
	return mcp.NewTool("codesearch_status",
		mcp.WithDescription("Report doc count, on-disk index size, schema version, and last sync time."),
		mcp.WithDestructiveHintAnnotation(false),
	)
}

func statusHandler(cfg *config.IndexConfig, lazy *index.LazyIndex) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		idx, release, err := lazy.Acquire()
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		defer release()

		count, err := idx.DocCount()
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		meta, err := sync.ReadMeta(cfg.MetaPath())
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		size, err := indexDirSize(cfg.IndexPath())
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		lastSync := ""
		if !meta.LastSyncAt.IsZero() {
			lastSync = meta.LastSyncAt.Format("2006-01-02T15:04:05Z07:00")
		}

		res := StatusResult{
			IndexPath:      cfg.IndexPath(),
			DocCount:       count,
			IndexSizeBytes: size,
			SchemaVersion:  meta.SchemaVersion,
			LastSyncAt:     lastSync,
		}
		return mcp.NewToolResultJSON(res)
	}
}

func indexDirSize(root string) (int64, error) {
	var total int64
	walk := func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			return infoErr
		}
		total += info.Size()
		return nil
	}

	err := filepath.WalkDir(root, walk)
	if err != nil {
		return 0, fmt.Errorf("walk index dir: %w", err)
	}
	return total, nil
}
