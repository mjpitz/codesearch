[CodeGraph][] has been an amazing addition to my developer workflow. Over the last few months, I
found a shortcoming or two that has left some gaps in my development workflow such as it's
inability to index markdown and common configuration languages. Don't get me wrong. CodeGraph is
AMAZING for performing refactorings, finding call sites, and navigating large source repositories.
However, codebases frequently contains non-code related artifacts, such as RFCs, designs, guides,
patterns, and even common configuration languages which are not handled by a tool like codegraph.

`codesearch` provides additional indexing and search capabilities on top of CodeGraph, allowing
Agents to query non-code related artifacts and navigate in-repository documentation.

[CodeGraph]: https://github.com/colbymchenry/codegraph

## Installation

```shell
go install github.com/mjpitz/codesearch@latest
```

## How I use this...

I use this tool in conjunction with `codegraph`, not stand alone. While it can be used standalone,
this solution takes an extremely naive approach to indexing any type of file within your repository.

### Integrating into Git

It took me a while to figure out the right way to have this all tied into my source code. I've
frequently been a bit hesitant to adopt githooks, but have come to find some appreciation for them
when trying to keep these types of indexes up to date. For each repo, I added the following snippet
to my `post-checkout` and `post-commit` hooks.

```sh
# post-checkout
# post-commit

if [ ! -d .codegraph ]; then
    codegraph init
fi

codegraph sync

if [ ! -d .codesearch ]; then
    codesearch init
fi

codesearch sync
```

This forces the indexes to populate on first clone, on branch change, and after committing active
work items. While uncommitted code may be indexed sporadically, the intent is to primarily index
code that's landed on a branch.

```sh
git config core.hooksPath .githooks
```

### Sporadic Queries

Every now and then, I need to sporadically query fairly large code bases outside the context of an
AI agent. `codesearch` offers a query operation that allows you to easily look up documents within
your repository.

```sh
codesearch query clickhouse

  1. services/clickhouse/README.md  (score 2.28)
    ClickHouse
    Columnar database that stores OpenTelemetry traces, logs, and metrics for the observability stack.

  2. services/grafana/infra/local/provisioning/datasources/clickhouse.yaml  (score 1.26)
    clickhouse
    clickhouse

  3. services/grafana/README.md  (score 0.83)
    Grafana
    Dashboarding and visualization layer that queries ClickHouse for traces, logs, and metrics across the observability stack.

  4. services/otel-collector/README.md  (score 0.27)
    OpenTelemetry Collector
    OTLP ingest and forwarding agent that receives telemetry from services and writes it to ClickHouse.
```

## Integrating into Agents

The primary way we integrate with Agents is using an MCP server and some guidance in `AGENTS.md` on
how to leverage the tooling available. `codegraph` will automatically add itself to
`~/.cluade/CLAUDE.md` however I personally prefer `AGENTS.md` since I tend to switch between a few
clients depending on the task at hand.

```json
{
  "mcpServers": {
    "codegraph": {
      "type": "stdio",
      "command": "codegraph",
      "args": ["serve", "--mcp"]
    },
    "codesearch": {
      "type": "stdio",
      "command": "codesearch",
      "args": ["serve", "--mcp"]
    }
  }
}
```

### Tools

The codesearch tool offers a number MCP tools that can be invoked across the repository.

| Tool              | Description                                                                                                                                           |
| ----------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------- |
| codesearch_search | Ranked full-text search across indexed docs and configs. Returns ranked hits with path, score, title, snippet.                                        |
| codesearch_facets | Return the distinct values seen in a given indexed field, with document counts. Use codesearch_fields to discover field names.                        |
| codesearch_fields | List every indexed field name in the codesearch index. Use the returned names with codesearch_facets or as keys in codesearch_search's fields filter. |
| codesearch_get    | Return the contents of a repo-relative path. Refuses absolute paths and `..` escapes. Truncates beyond max_bytes (default 65536, hard cap 262144).    |
| codesearch_status | Report doc count, on-disk index size, schema version, and last sync time.                                                                             |

## Page Rank - Boosts

Markdown files have a distinct advantage in this ecosystem due to their support for frontmatter.
As a result, `codesearch` attempts to to parse out YAML frontmatter and extract common fields
related to SEO.

```go
// DefaultBoosts are the query-time field boosts applied when an indexed
// field is present.
var DefaultBoosts = map[string]float64{
	"title":       5.0,
	"tags":        2.0,
	"body":        1.0,
	"description": 3.0,
	"keywords":    4.0,
}
```

## Known Limitations

- Due to our use of a Bleve index under the hood, there can only be one active process reading the
  index at a time. To mitigate this issue, our MCP server maintains a lazy reference that's opened
  at the start of a request when no existing handle exists and closed after a specific idle window
  (30s).
