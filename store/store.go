// Package store: everything archive-stt keeps, under the data dir, never expired:
//
//	archive-stt.db                    SQLite: runs (run), their diarization (diarization) and the lesson's
//	                                  segmentation revisions (segmentation)
//	words/{file_uid}/{run_id}.json    the media file's words with their times ({"words": [{word, start, end}]}):
//	                                  the immutable layer. The same file may be transcribed again by another
//	                                  model -- a new run_id next to the old one.
//	align/{transcript_file_uid}/{run_id}.json
//	                                  the transcript's words on the run's words (models.Alignment): the time of
//	                                  every word of the transcript, for search. Derived, written with every
//	                                  segmentation built on that transcript and run.
//
// Words + diarization are the input of a rebuild of the automatic segmentation.
//
// Lessons and files are MDB's (through kmedia): nothing of them is kept but the file_uid the data is built from.
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
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
	DB  *sql.DB // the writer: one connection, SQLite takes one writer at a time
	RO  *sql.DB // the readers: WAL lets them read while the writer writes
}

const readers = 8

// Open opens (creating) the store in dir and applies the migrations
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	file := "file:" + filepath.Join(dir, "archive-stt.db") + "?_pragma=busy_timeout(30000)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", file+"&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	// one writer at a time: SQLite's own rule, kept here so a transaction never meets SQLITE_BUSY
	db.SetMaxOpenConns(1)
	if err := migrations.Apply(db); err != nil {
		db.Close()
		return nil, err
	}
	// opened after the migrations (WAL is set on the file by then); query_only: a write here is an error
	ro, err := sql.Open("sqlite", file+"&_pragma=query_only(1)")
	if err != nil {
		db.Close()
		return nil, err
	}
	ro.SetMaxOpenConns(readers)
	ro.SetMaxIdleConns(readers)
	return &Store{Dir: dir, DB: db, RO: ro}, nil
}

func (s *Store) Close() error {
	return errors.Join(s.RO.Close(), s.DB.Close())
}

// Path is a path under the data dir
func (s *Store) Path(parts ...string) string {
	return filepath.Join(append([]string{s.Dir}, parts...)...)
}

// words: the immutable layer

func (s *Store) WordsPath(fileUID, runID string) string {
	return s.Path("words", fileUID, runID+".json")
}

// SaveWords writes the run's words file
func (s *Store) SaveWords(fileUID, runID string, w *models.Words) error {
	return utils.WriteJSON(s.WordsPath(fileUID, runID), w)
}

func (s *Store) LoadWords(fileUID, runID string) (*models.Words, error) {
	b, err := os.ReadFile(s.WordsPath(fileUID, runID))
	if err != nil {
		return nil, err
	}
	w := new(models.Words)
	if err := json.Unmarshal(b, w); err != nil {
		return nil, fmt.Errorf("words %s/%s: %w", fileUID, runID, err)
	}
	return w, nil
}

// alignment: the transcript's words on a run's

func (s *Store) AlignmentPath(transcriptUID, runID string) string {
	return s.Path("align", transcriptUID, runID+".json")
}

func (s *Store) SaveAlignment(transcriptUID, runID string, a *models.Alignment) error {
	return utils.WriteJSON(s.AlignmentPath(transcriptUID, runID), a)
}

// LoadAlignment is the alignment, or nil if there is none
func (s *Store) LoadAlignment(transcriptUID, runID string) (*models.Alignment, error) {
	b, err := os.ReadFile(s.AlignmentPath(transcriptUID, runID))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	a := new(models.Alignment)
	if err := json.Unmarshal(b, a); err != nil {
		return nil, fmt.Errorf("alignment %s/%s: %w", transcriptUID, runID, err)
	}
	return a, nil
}

// diarization: immutable, one per run

func (s *Store) SaveDiarization(runID string, d *models.Diarization) error {
	data, err := json.Marshal(d)
	if err != nil {
		return err
	}
	_, err = s.DB.Exec("INSERT INTO diarization (run_id, data) VALUES (?, ?)", runID, string(data))
	return err
}

// Diarization is the run's diarization, or nil
func (s *Store) Diarization(runID string) (*models.Diarization, error) {
	var data string
	err := s.RO.QueryRow("SELECT data FROM diarization WHERE run_id = ?", runID).Scan(&data)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	d := new(models.Diarization)
	if err := json.Unmarshal([]byte(data), d); err != nil {
		return nil, fmt.Errorf("diarization %s: %w", runID, err)
	}
	return d, nil
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
	return scanRun(s.RO.QueryRow("SELECT "+runCols+" FROM run WHERE id = ?", id))
}

// LatestRun is the lesson's newest run, or nil
func (s *Store) LatestRun(cuUID string) (*models.Run, error) {
	return scanRun(s.RO.QueryRow("SELECT "+runCols+" FROM run WHERE cu_uid = ? ORDER BY created DESC, rowid DESC "+
		"LIMIT 1", cuUID))
}

// Runs are the lesson's runs, newest first
func (s *Store) Runs(cuUID string) ([]*models.Run, error) {
	rows, err := s.RO.Query("SELECT "+runCols+" FROM run WHERE cu_uid = ? ORDER BY created DESC, rowid DESC", cuUID)
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
	rows, err := s.RO.Query("SELECT DISTINCT cu_uid FROM run WHERE status = 'pending'")
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
	err := s.RO.QueryRow("SELECT COUNT(*) FROM run").Scan(&n)
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

// LastAuto is the lesson's last revision auto built on the run, or nil
func (s *Store) LastAuto(cuUID, runID string) (*models.Segmentation, error) {
	return scanSegmentation(s.RO.QueryRow("SELECT "+segCols+" FROM segmentation WHERE cu_uid = ? AND "+
		"words_run_id = ? AND source = 'auto' ORDER BY rev DESC LIMIT 1", cuUID, runID))
}

// Segmentation is the given revision (rev > 0) or the current one: the last human / migrated if any, else the
// last auto; nil if none
func (s *Store) Segmentation(cuUID string, rev int) (*models.Segmentation, error) {
	if rev > 0 {
		return scanSegmentation(s.RO.QueryRow("SELECT "+segCols+" FROM segmentation WHERE cu_uid = ? AND rev = ?",
			cuUID, rev))
	}
	return scanSegmentation(s.RO.QueryRow("SELECT "+segCols+" FROM segmentation WHERE cu_uid = ? "+
		"ORDER BY source = 'auto', rev DESC LIMIT 1", cuUID))
}

// WithoutTranscript are the content units whose current revision has no transcript (built from the audio), in language
// ("": any): uid and the language of the revision's run, by uid
func (s *Store) WithoutTranscript(language string) ([]models.UnitRef, error) {
	rows, err := s.RO.Query(`
SELECT g.cu_uid, r.language
FROM (SELECT cu_uid, words_run_id, data,
             ROW_NUMBER() OVER (PARTITION BY cu_uid ORDER BY source = 'auto', rev DESC) AS n
      FROM segmentation) g
    JOIN run r ON r.id = g.words_run_id
WHERE g.n = 1 AND json_extract(g.data, '$.transcript') IS NULL AND (? = '' OR r.language = ?)
ORDER BY g.cu_uid`, language, language)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.UnitRef, 0)
	for rows.Next() {
		var l models.UnitRef
		if err := rows.Scan(&l.UID, &l.Language); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
