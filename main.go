package main

import (
	"context"
	"fmt"
	"os"

	"github.com/mjpitz/codesearch/internal/cmd"
	"github.com/urfave/cli/v3"
)

func main() {
	cmd := &cli.Command{
		Name:        "codesearch",
		Description: "Quickly and easily search large code bases for relevant documentation.",
		Commands: []*cli.Command{
			cmd.Init,
			cmd.Facets,
			cmd.Fields,
			cmd.Query,
			cmd.Serve,
			cmd.Status,
			cmd.Sync,
		},
	}

	err := cmd.Run(context.Background(), os.Args)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
