package store

import (
	"embed"
	"io/fs"
)

//go:embed migrations/*.sql
var raw embed.FS

// Migrations is the embedded migration FS with the "migrations/" prefix stripped,
// so goose sees the .sql files at the root.
var Migrations, _ = fs.Sub(raw, "migrations")
