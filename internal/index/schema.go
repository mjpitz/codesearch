// Package index wraps Bleve: schema definition, Open/Create, and the
// Upsert/Delete/Stats surface used by sync, query, and MCP layers.
package index

import (
	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/mapping"
)

// SchemaVersion is bumped when the field mapping changes in a
// backwards-incompatible way. sync compares it against meta.json and
// rebuilds the index on mismatch.
const SchemaVersion = 1

// BuildMapping returns the Bleve index mapping. The default document
// mapping has Dynamic enabled so any frontmatter key becomes an indexed
// field automatically; the named fields below are pinned so they have
// consistent analyzers regardless of what shows up in dynamic mode.
func BuildMapping() *mapping.IndexMappingImpl {
	keyword := bleve.NewTextFieldMapping()
	keyword.Analyzer = "keyword"
	keyword.Store = true

	text := bleve.NewTextFieldMapping()
	text.Analyzer = "en"
	text.Store = true

	body := bleve.NewTextFieldMapping()
	body.Analyzer = "en"
	body.Store = false
	body.IncludeTermVectors = true

	number := bleve.NewNumericFieldMapping()
	number.Store = true

	doc := bleve.NewDocumentMapping()
	doc.Dynamic = true

	doc.AddFieldMappingsAt("path", keyword)
	doc.AddFieldMappingsAt("title", text)
	doc.AddFieldMappingsAt("tags", keyword)
	doc.AddFieldMappingsAt("body", body)
	doc.AddFieldMappingsAt("digest", keyword)
	doc.AddFieldMappingsAt("mtime", number)
	doc.AddFieldMappingsAt("size", number)

	m := bleve.NewIndexMapping()
	m.DefaultMapping = doc
	return m
}
