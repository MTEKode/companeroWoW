package store

import (
	_ "embed"
)

// viewsDoc is the schema doc of the read-only views (VIEWS.md, in Spanish). The consultar_db tool
// uses it as its description, so the model reads the columns it may query.
//
//go:embed VIEWS.md
var viewsDoc string

// ViewsDoc returns the schema doc of the read-only views.
func ViewsDoc() string { return viewsDoc }
