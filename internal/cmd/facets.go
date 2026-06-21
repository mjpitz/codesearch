package cmd

import (
	"context"
	"fmt"

	"github.com/blevesearch/bleve/v2"
	"github.com/mjpitz/codesearch/internal/config"
	"github.com/mjpitz/codesearch/internal/index"
	"github.com/urfave/cli/v3"
)

var Facets = &cli.Command{
	Name:  "facets",
	Usage: "Return the distinct values seen in a given indexed field.",
	Flags: []cli.Flag{
		&cli.IntFlag{
			Name:  "limit",
			Usage: "max facets to return",
			Value: 10,
		},
	},
	Arguments: []cli.Argument{
		&cli.StringArg{
			Name:      "field",
			UsageText: "the name of the field to lookup",
		},
	},
	Action: func(ctx context.Context, c *cli.Command) error {
		limit := c.Value("limit").(int)
		field := c.StringArg("field")

		cfg, err := config.Load("")
		if err != nil {
			return err
		}
		idx, err := index.OpenReadOnly(cfg.IndexPath())
		if err != nil {
			return err
		}
		defer func() { _ = idx.Close() }()

		bleveReq := bleve.NewSearchRequestOptions(bleve.NewMatchAllQuery(), 0, 0, false)
		bleveReq.AddFacet(field, bleve.NewFacetRequest(field, limit))

		result, err := idx.Bleve().Search(bleveReq)
		if err != nil {
			return err
		}

		facet, ok := result.Facets[field]
		if !ok || facet == nil {
			return nil
		}

		for _, t := range facet.Terms.Terms() {
			fmt.Printf("%s\t%d\n", t.Term, t.Count)
		}

		return nil
	},
}
