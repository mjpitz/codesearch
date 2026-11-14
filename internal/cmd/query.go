package cmd

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"sort"
	"strings"

	chromem "github.com/philippgille/chromem-go"

	"github.com/mjpitz/codesearch/internal/config"
	"github.com/mjpitz/codesearch/internal/index"
	"github.com/mjpitz/codesearch/internal/query"
	"github.com/mjpitz/codesearch/internal/sync"
	"github.com/urfave/cli/v3"
)

// Query searches the index for the given terms.
var Query = &cli.Command{
	Name:  "query",
	Usage: "Search the index for the given terms.",
	Flags: []cli.Flag{
		&cli.StringMapFlag{
			Name:  "fields",
			Usage: "filter `key=value` (repeatable, AND)",
		},
		&cli.IntFlag{
			Name:  "limit",
			Usage: "max hits to return",
			Value: 10,
		},
		&cli.StringFlag{
			Name:  "format",
			Usage: "output format: text or json",
			Value: "text",
		},
		&cli.BoolFlag{
			Name:  "no-snippet",
			Usage: "skip snippet generation",
			Value: false,
		},
	},
	Action: func(ctx context.Context, c *cli.Command) error {
		fields := c.Value("fields").(map[string]string)
		limit := c.Value("limit").(int)
		format := c.Value("format").(string)
		noSnippet := c.Value("no-snippet").(bool)

		terms := strings.Join(c.Args().Slice(), " ")

		cfg, err := config.Load("")
		if err != nil {
			return err
		}

		idx, err := index.OpenReadOnly(cfg.IndexPath())
		if err != nil {
			return err
		}
		defer func() { _ = idx.Close() }()

		req := query.Request{
			Terms:     terms,
			Fields:    fields,
			Limit:     limit,
			NoSnippet: noSnippet,
			Boosts:    cfg.Boosts,
		}

		switch format {
		case "json":
			req.Highlight = "html"
		default:
			req.Highlight = "ansi"
		}

		// Attempt vector search when embedder env vars and a populated vector
		// store are both present. Falls back to text-only on any error.
		meta, _ := sync.ReadMeta(cfg.MetaPath())
		if meta.VectorDim > 0 && terms != "" {
			embedder, embErr := index.NewEmbedder()
			if embErr != nil {
				log.Printf("embedder init: %v (text-only)", embErr)
			} else if embedder != nil {
				req.VectorRanking = vectorRanking(ctx, cfg.VectorStorePath(), embedder, terms, limit*2)
			}
		}

		res, err := query.Search(idx, req)
		if err != nil {
			return err
		}

		if format == "json" {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(res)
		}

		return query.RenderText(os.Stdout, res)
	},
}

// vectorRanking opens the chromem-go vector store, embeds the query, and
// returns a path→rank map (1-indexed). Returns nil on any error so the
// caller can fall back to text-only search.
func vectorRanking(ctx context.Context, storePath string, embedder index.Embedder, terms string, n int) map[string]int {
	db, err := chromem.NewPersistentDB(storePath, false)
	if err != nil {
		log.Printf("vector store open: %v (text-only)", err)
		return nil
	}

	col := db.GetCollection("codesearch", nil)
	if col == nil || col.Count() == 0 {
		return nil
	}

	queryVec, err := embedder.Embed(ctx, terms)
	if err != nil {
		log.Printf("query embed: %v (text-only)", err)
		return nil
	}

	results, err := col.QueryEmbedding(ctx, queryVec, n, nil, nil)
	if err != nil {
		log.Printf("vector query: %v (text-only)", err)
		return nil
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Similarity > results[j].Similarity
	})

	ranking := make(map[string]int, len(results))
	for i, r := range results {
		ranking[r.ID] = i + 1
	}

	return ranking
}
