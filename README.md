# CodeSearch

[CodeGraph][] is great, but it only handles code and codebases are composed of more than code.
While CodeGraph makes it easier to locate symbols, track callers, and so many other useful coding
features, CodeSearch enables free-text search across all types of files allowing it to locate
documentation (guides, patterns, and more) as well as configuration (such as Helm or Terraform).

We follow a similar model as CodeGraph, writing a localized [Bleve][] index to `.codesearch` and
exposing an MCP server for Agents like Claude, Cline, Cursor, and so many more to call.

[CodeGraph]: https://github.com/colbymchenry/codegraph
[Bleve]: https://blevesearch.com/

## Installation

```shell
go install github.com/mjpitz/codesearch@latest
```

## Configuring Agents

Despite writing this tool, I still leverage both codegraph ad codesearch.

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

## MCP Tools

```json
{
  "permissions": {
    "allow": [
      "mcp__codesearch__codesearch_search",
      "mcp__codesearch__codesearch_facets",
      "mcp__codesearch__codesearch_fields",
      "mcp__codesearch__codesearch_get",
      "mcp__codesearch__codesearch_status"
    ]
  }
}
```
