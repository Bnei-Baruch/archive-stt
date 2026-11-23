// Package migrations: the SQLite schema, applied in file name order at every start (each is idempotent).
package migrations

import (
	"database/sql"
	"embed"
	"fmt"
	"sort"
)

//go:embed *.sql
var files embed.FS

// Apply runs every migration in name order
func Apply(db *sql.DB) error {
	entries, err := files.ReadDir(".")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	for _, name := range names {
		b, err := files.ReadFile(name)
		if err != nil {
			return err
		}
		if _, err := db.Exec(string(b)); err != nil {
			return fmt.Errorf("migration %s: %w", name, err)
		}
	}
	return nil
}
