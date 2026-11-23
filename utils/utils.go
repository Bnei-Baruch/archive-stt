// Package utils: small helpers.
package utils

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Must panics on an error: for setup that cannot go on without it
func Must(err error) {
	if err != nil {
		panic(err)
	}
}

// Now is the current UTC time as ISO 8601 with seconds ("2026-11-15T11:30:26+00:00")
func Now() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05-07:00")
}

// WriteJSON writes v to p atomically (a temp file renamed over it), creating the directories
func WriteJSON(p string, v interface{}) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(p+".tmp", b, 0o644); err != nil {
		return err
	}
	return os.Rename(p+".tmp", p)
}
