// Package common holds the values shared by the packages: speaker roles, run and revision states.
package common

// Speaker roles: codes, the client draws the labels
const (
	RoleRav     = "rav"
	RoleReader  = "reader"
	RoleStudent = "student"
)

// Run kinds
const (
	RunKindFirst = "first" // the lesson's first markup
	RunKindFile  = "file"  // the original file was replaced
	RunKindModel = "model" // the same file, a newer STT model
)

// Run stages and statuses
const (
	StageWords    = "words"
	StageSpeakers = "speakers"

	StatusPending = "pending"
	StatusReady   = "ready"
	StatusFailed  = "failed"
)

// Where a run's words come from
const (
	WordsSourceSTT = "stt"
	WordsSourceVTT = "vtt"
)

// Segmentation revision sources
const (
	RevAuto     = "auto"
	RevHuman    = "human"
	RevMigrated = "migrated"
)

// What a segmentation is built on
const (
	SegFromTranscript = "transcript"
	SegFromAudio      = "audio"
)

// Languages ai-trns transcribes
var Languages = []string{"he", "en", "ru"}

func IsLanguage(lang string) bool {
	for _, l := range Languages {
		if l == lang {
			return true
		}
	}
	return false
}
