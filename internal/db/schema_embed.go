package db

import _ "embed"

// Schema contains the raw SQL schema for the Watchtower database.
// Exported for use in AI prompts so Claude can query the DB directly.
//
//go:embed schema.sql
var Schema string
