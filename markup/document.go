package markup

import (
	"github.com/Bnei-Baruch/archive-stt/common"
	"github.com/Bnei-Baruch/archive-stt/models"
)

// Version of the markup document
const Version = 2

// Document is the markup served (GET /cus/{uid}/markup): a segmentation revision and the run whose words it is
// on.
//
//	{
//	  "version": 2, "uid", "original_language", "language",
//	  "media":        {"file_uid", "duration_s"},
//	  "transcript":   {"file_uid"} | null,        // the kmedia transcript (archive-unzip doc2html) it is built on
//	  "run":          {"id", "created", "words_source": "stt" | "vtt", "source": "transcript" | "audio"},
//	  "segmentation": {"rev", "source": "auto" | "human" | "migrated", "created"},
//	  "paragraphs":   [{"i", "start", "end", "speaker"}],      // speaker: rav | reader | student | null
//	  "chapters":     [{"type", "title", "start", "end", "p_start", "p_end"}],
//	  "stats":        {...}
//	}
func Document(seg *models.Segmentation, run *models.Run) *models.Document {
	orig := run.Data.OriginalLanguage
	if orig == "" {
		orig = run.Language
	}
	var transcript *models.FileRef
	if seg.Data.Source == common.SegFromTranscript {
		transcript = run.Data.Transcript
	}
	stats := seg.Data.Stats
	if stats == nil {
		stats = map[string]interface{}{}
	}
	return &models.Document{
		Version: Version, UID: seg.CuUID, OriginalLanguage: orig, Language: run.Language,
		Media: run.Data.Media, Transcript: transcript,
		Run: models.DocumentRun{ID: run.ID, Created: run.Created, WordsSource: run.WordsSource,
			Source: seg.Data.Source},
		Segmentation: models.DocumentSegmentation{Rev: seg.Rev, Source: seg.Source, Created: seg.Created},
		Paragraphs:   nonNil(seg.Data.Paragraphs), Chapters: nonNilC(seg.Data.Chapters), Stats: stats,
	}
}

func nonNil(ps []models.Paragraph) []models.Paragraph {
	if ps == nil {
		return []models.Paragraph{}
	}
	return ps
}

func nonNilC(cs []models.Chapter) []models.Chapter {
	if cs == nil {
		return []models.Chapter{}
	}
	return cs
}
