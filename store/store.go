// Package store: everything archive-stt keeps, under the data dir, never expired:
//
//	archive-stt.db                    SQLite: runs (run) and the lesson's segmentation revisions (segmentation)
//	words/{file_uid}/{run_id}.json    what ai-trns answered for that media file (paragraphs with words, times,
//	                                  speakers): the immutable layer, the input of a rebuild. The same file may be
//	                                  transcribed again by another model -- a new run_id next to the old one.
//
// Lessons and files are MDB's (through kmedia): nothing of them is kept but the file_uid the data is built from.
package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"

	"github.com/Bnei-Baruch/archive-stt/migrations"
	"github.com/Bnei-Baruch/archive-stt/models"
	"github.com/Bnei-Baruch/archive-stt/utils"
)

type Store struct {
	Dir string
	DB  *sql.DB
}

// Open opens (creating) the store in dir and applies the migrations
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	dsn := "file:" + filepath.Join(dir, "archive-stt.db") +
		"?_pragma=busy_timeout(30000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// one writer at a time: SQLite's own rule, kept here so a transaction never meets SQLITE_BUSY
	db.SetMaxOpenConns(1)
	if err := migrations.Apply(db); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{Dir: dir, DB: db}, nil
}

func (s *Store) Close() error {
	return s.DB.Close()
}

// Path is a path under the data dir
func (s *Store) Path(parts ...string) string {
	return filepath.Join(append([]string{s.Dir}, parts...)...)
}

// words: the immutable layer

func (s *Store) WordsPath(fileUID, runID string) string {
	return s.Path("words", fileUID, runID+".json")
}

// SaveWords keeps ai-trns's answer as it came
func (s *Store) SaveWords(fileUID, runID string, raw json.RawMessage) error {
	return utils.WriteJSON(s.WordsPath(fileUID, runID), raw)
}

func (s *Store) LoadWords(fileUID, runID string) (*models.DiarizeResult, error) {
	b, err := os.ReadFile(s.WordsPath(fileUID, runID))
	if err != nil {
		return nil, err
	}
	return models.ParseDiarizeResult(b)
}

// runs

const runCols = "id, cu_uid, kind, language, file_uid, stage, status, words_source, vtt_file_uid, error, jobs, " +
	"data, author, created, updated"

type scanner interface {
	Scan(dest ...interface{}) error
}

func scanRun(row scanner) (*models.Run, error) {
	r := new(models.Run)
	var jobs, data string
	err := row.Scan(&r.ID, &r.CuUID, &r.Kind, &r.Language, &r.FileUID, &r.Stage, &r.Status, &r.WordsSource,
		&r.VttFileUID, &r.Error, &jobs, &data, &r.Author, &r.Created, &r.Updated)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(jobs), &r.Jobs); err != nil {
		return nil, fmt.Errorf("run %s jobs: %w", r.ID, err)
	}
	if err := json.Unmarshal([]byte(data), &r.Data); err != nil {
		return nil, fmt.Errorf("run %s data: %w", r.ID, err)
	}
	return r, nil
}

// SaveRun inserts or replaces the run; Updated is set here
func (s *Store) SaveRun(r *models.Run) error {
	r.Updated = utils.Now()
	jobs, err := json.Marshal(r.Jobs)
	if err != nil {
		return err
	}
	data, err := json.Marshal(r.Data)
	if err != nil {
		return err
	}
	_, err = s.DB.Exec("INSERT OR REPLACE INTO run ("+runCols+") VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		r.ID, r.CuUID, r.Kind, r.Language, r.FileUID, r.Stage, r.Status, r.WordsSource, r.VttFileUID, r.Error,
		string(jobs), string(data), r.Author, r.Created, r.Updated)
	return err
}

// GetRun is the run, or nil
func (s *Store) GetRun(id string) (*models.Run, error) {
	return scanRun(s.DB.QueryRow("SELECT "+runCols+" FROM run WHERE id = ?", id))
}

// LatestRun is the lesson's newest run, or nil
func (s *Store) LatestRun(cuUID string) (*models.Run, error) {
	return scanRun(s.DB.QueryRow("SELECT "+runCols+" FROM run WHERE cu_uid = ? ORDER BY created DESC, rowid DESC "+
		"LIMIT 1", cuUID))
}

// Runs are the lesson's runs, newest first
func (s *Store) Runs(cuUID string) ([]*models.Run, error) {
	rows, err := s.DB.Query("SELECT "+runCols+" FROM run WHERE cu_uid = ? ORDER BY created DESC, rowid DESC", cuUID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*models.Run, 0)
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// PendingUIDs are the lessons with a pending run
func (s *Store) PendingUIDs() ([]string, error) {
	rows, err := s.DB.Query("SELECT DISTINCT cu_uid FROM run WHERE status = 'pending'")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			return nil, err
		}
		out = append(out, uid)
	}
	return out, rows.Err()
}

func (s *Store) CountRuns() (int, error) {
	var n int
	err := s.DB.QueryRow("SELECT COUNT(*) FROM run").Scan(&n)
	return n, err
}

// segmentation revisions

const segCols = "cu_uid, rev, file_uid, words_run_id, source, based_on, author, created, data"

func scanSegmentation(row scanner) (*models.Segmentation, error) {
	g := new(models.Segmentation)
	var data string
	err := row.Scan(&g.CuUID, &g.Rev, &g.FileUID, &g.WordsRunID, &g.Source, &g.BasedOn, &g.Author, &g.Created, &data)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(data), &g.Data); err != nil {
		return nil, fmt.Errorf("segmentation %s rev %d: %w", g.CuUID, g.Rev, err)
	}
	return g, nil
}

// AddSegmentation writes a new revision of the lesson's segmentation: Rev = the last + 1 (set here); Created is
// set if empty
func (s *Store) AddSegmentation(g *models.Segmentation) error {
	if g.Created == "" {
		g.Created = utils.Now()
	}
	data, err := json.Marshal(g.Data)
	if err != nil {
		return err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := tx.QueryRow("SELECT COALESCE(MAX(rev), 0) + 1 FROM segmentation WHERE cu_uid = ?", g.CuUID).
		Scan(&g.Rev); err != nil {
		return err
	}
	if _, err := tx.Exec("INSERT INTO segmentation ("+segCols+") VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		g.CuUID, g.Rev, g.FileUID, g.WordsRunID, g.Source, g.BasedOn, g.Author, g.Created, string(data)); err != nil {
		return err
	}
	return tx.Commit()
}

// Segmentation is the given revision (rev > 0) or the current one: the last human / migrated if any, else the
// last auto; nil if none
func (s *Store) Segmentation(cuUID string, rev int) (*models.Segmentation, error) {
	if rev > 0 {
		return scanSegmentation(s.DB.QueryRow("SELECT "+segCols+" FROM segmentation WHERE cu_uid = ? AND rev = ?",
			cuUID, rev))
	}
	return scanSegmentation(s.DB.QueryRow("SELECT "+segCols+" FROM segmentation WHERE cu_uid = ? "+
		"ORDER BY source = 'auto', rev DESC LIMIT 1", cuUID))
}
