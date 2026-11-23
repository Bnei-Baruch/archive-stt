// Package mdb: what archive-stt reads from MDB directly (read only), where kmedia's API does not tell it: the
// lesson's subtitles files with the date they were made.
package mdb

import (
	"database/sql"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// VTTBefore: subtitles made before it are checked by people -- a dataset the STT model is trained on; the words of
// a lesson that has them are taken from them, not from the STT. Later ones are not used.
const VTTBefore = "2027-01-01"

type Client struct {
	DB *sql.DB
}

// Open connects to MDB (postgres://...); nothing is asked until the first query
func Open(url string) (*Client, error) {
	db, err := sql.Open("pgx", url)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(2)
	db.SetConnMaxIdleTime(5 * time.Minute)
	return &Client{DB: db}, nil
}

func (c *Client) Close() error {
	return c.DB.Close()
}

// VTT is the lesson's subtitles file in language made before VTTBefore (published, not secure, not removed; the
// newest if there are a few), or ""
func (c *Client) VTT(cuUID, language string) (string, error) {
	var uid string
	err := c.DB.QueryRow(`
SELECT f.uid FROM files f
JOIN content_units cu ON cu.id = f.content_unit_id
WHERE cu.uid = $1 AND f.language = $2
  AND f.properties->>'insert_type' = 'subtitles'
  AND f.published AND f.secure = 0 AND f.removed_at IS NULL
  AND f.created_at < $3::date
ORDER BY f.created_at DESC
LIMIT 1`, cuUID, language, VTTBefore).Scan(&uid)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return uid, err
}
