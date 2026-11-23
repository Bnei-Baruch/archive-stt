package store

import (
	"fmt"
	"testing"
	"time"

	"github.com/Bnei-Baruch/archive-stt/migrations"
)

// A revision written when the run named the transcript gets it from the run; the run no longer has it
func TestMigrationMovesTheTranscriptFromTheRunToTheRevision(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, q := range []string{
		`INSERT INTO run (id, cu_uid, kind, language, file_uid, stage, status, words_source, data, created, updated)
		 VALUES ('r1', 'Unit0001', 'first', 'he', 'AudioOr1', 'speakers', 'ready', 'stt',
		         '{"media":{"file_uid":"AudioOr1"},"transcript":{"file_uid":"TextDoc1"}}', 'x', 'x')`,
		`INSERT INTO segmentation (cu_uid, rev, file_uid, words_run_id, source, created, data) VALUES
		 ('Unit0001', 1, 'AudioOr1', 'r1', 'auto', 'x', '{"source":"transcript","paragraphs":[]}'),
		 ('Unit0001', 2, 'AudioOr1', 'r1', 'auto', 'x', '{"source":"audio","paragraphs":[]}'),
		 ('Unit0001', 3, 'AudioOr1', 'r1', 'auto', 'x', '{"source":"transcript","transcript":{"file_uid":"TextNew1"}}')`,
	} {
		if _, err := s.DB.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ { // at every start: idempotent
		if err := migrations.Apply(s.DB); err != nil {
			t.Fatal(err)
		}
	}
	want := map[int]string{1: "TextDoc1", 2: "", 3: "TextNew1"}
	for rev, uid := range want {
		g, err := s.Segmentation("Unit0001", rev)
		if err != nil {
			t.Fatal(err)
		}
		got := ""
		if g.Data.Transcript != nil {
			got = g.Data.Transcript.FileUID
		}
		if got != uid {
			t.Fatalf("rev %d: %q, want %q", rev, got, uid)
		}
	}
	var data string
	s.DB.QueryRow("SELECT data FROM run WHERE id = 'r1'").Scan(&data)
	if data != `{"media":{"file_uid":"AudioOr1"}}` {
		t.Fatal(data)
	}
}

// Readers do not wait for the writer: a read goes through while a write transaction is open; and they cannot write
func TestReadersDoNotWaitForTheWriter(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	tx, err := s.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO run (id, cu_uid, kind, language, file_uid, stage, status, words_source, created,
		updated) VALUES ('r1', 'Unit0001', 'first', 'he', 'AudioOr1', 'words', 'pending', 'stt', 'x', 'x')`); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		n, err := s.CountRuns()
		if err == nil && n != 0 {
			err = fmt.Errorf("%d runs before the commit", n)
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the read waits for the writer")
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if n, err := s.CountRuns(); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if _, err := s.RO.Exec("DELETE FROM run"); err == nil {
		t.Fatal("a reader wrote")
	}
}
