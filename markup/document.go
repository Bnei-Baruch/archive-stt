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
//	  "paragraphs":   [{"i", "start", "end", "speaker", "text"?}], // speaker: rav | reader | student | null;
//	                                                           // text: source "audio" only, from the run's words
//	  "chapters":     [{"type", "title", "start", "end", "p_start", "p_end"}],
//	  "stats":        {...}
//	}
//
// words: the run's words, for the paragraphs' text when there is no transcript (nil: no text)
func Document(seg *models.Segmentation, run *models.Run, words *models.Words) *models.Document {
	orig := run.Data.OriginalLanguage
	if orig == "" {
		orig = run.Language
	}
	var transcript *models.FileRef
	if seg.Data.Source == common.SegFromTranscript {
		transcript = seg.Data.Transcript
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
		Paragraphs:   withText(nonNil(seg.Data.Paragraphs), seg.Data.Source, words), Chapters: nonNilC(seg.Data.Chapters),
		Stats: stats,
	}
}

// withText is a copy of the paragraphs with their words' text, when they are built from the audio
func withText(ps []models.Paragraph, source string, words *models.Words) []models.Paragraph {
	if source != common.SegFromAudio || words == nil {
		return ps
	}
	starts := make([]float64, len(ps))
	for i, p := range ps {
		starts[i] = p.Start
	}
	out := make([]models.Paragraph, len(ps))
	for i, ws := range Split(starts, words.Words) {
		t := Text(ws)
		out[i] = ps[i]
		out[i].Text = &t
	}
	return out
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
