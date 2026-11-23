// Package models: the data archive-stt keeps and serves.
package models

// FileRef is a kmedia (MDB) file by its uid
type FileRef struct {
	FileUID string `json:"file_uid"`
}

// Media is the media file a run transcribed
type Media struct {
	FileUID   string  `json:"file_uid"`
	DurationS float64 `json:"duration_s,omitempty"`
}

// Jobs are a run's ai-trns jobs
type Jobs struct {
	Words    string `json:"words,omitempty"`
	Speakers string `json:"speakers,omitempty"`
}

// RunData is the rest of a run: what it was started on and what it gave
type RunData struct {
	AudioURL         string                 `json:"audio_url,omitempty"`
	OriginalLanguage string                 `json:"original_language,omitempty"`
	Media            Media                  `json:"media"`
	Finished         string                 `json:"finished,omitempty"`
	Stats            map[string]interface{} `json:"stats,omitempty"`
}

// Run is one pass over a lesson's media file: ai-trns words -> speakers -> a segmentation revision `auto`
type Run struct {
	ID          string  `json:"id"`
	CuUID       string  `json:"cu_uid"`
	Kind        string  `json:"kind"`
	Language    string  `json:"language"`
	FileUID     string  `json:"file_uid"`
	Stage       string  `json:"stage"`
	Status      string  `json:"status"`
	WordsSource string  `json:"words_source"`
	VttFileUID  *string `json:"vtt_file_uid"`
	Error       *string `json:"error"`
	Jobs        Jobs    `json:"jobs"`
	Data        RunData `json:"data"`
	Author      *string `json:"author"`
	Created     string  `json:"created"`
	Updated     string  `json:"updated"`
}

// Paragraph is a paragraph on the media's time line. I: the index of the <p> in the transcript's doc2html, or of
// the diarize paragraph when there is no transcript. Speaker: a role code or nil. Text is not stored: the document
// served fills it from the run's words when there is no transcript.
type Paragraph struct {
	I       int     `json:"i"`
	Start   float64 `json:"start"`
	End     float64 `json:"end"`
	Speaker *string `json:"speaker"`
	Text    *string `json:"text,omitempty"`
}

// Chapter is a part of the lesson: reading | commentary | qa | other; PStart / PEnd: its paragraphs' I
type Chapter struct {
	Type   string  `json:"type"`
	Title  string  `json:"title"`
	Start  float64 `json:"start"`
	End    float64 `json:"end"`
	PStart int     `json:"p_start"`
	PEnd   int     `json:"p_end"`
}

// SegmentationData is the content of a segmentation revision
type SegmentationData struct {
	Source     string                 `json:"source"`               // transcript | audio
	Transcript *FileRef               `json:"transcript,omitempty"` // the kmedia transcript it is built on
	Paragraphs []Paragraph            `json:"paragraphs"`
	Chapters   []Chapter              `json:"chapters"`
	Stats      map[string]interface{} `json:"stats"`
}

// Segmentation is a revision of the lesson's paragraphs, speakers and chapters
type Segmentation struct {
	CuUID      string           `json:"cu_uid"`
	Rev        int              `json:"rev"`
	FileUID    string           `json:"file_uid"`
	WordsRunID string           `json:"words_run_id"`
	Source     string           `json:"source"` // auto | human | migrated
	BasedOn    *int             `json:"based_on"`
	Author     *string          `json:"author"`
	Created    string           `json:"created"`
	Data       SegmentationData `json:"data"`
}

// Word is a heard word with its time
type Word struct {
	Word  string  `json:"word"`
	Start float64 `json:"start"`
	End   float64 `json:"end"`
}

// Words is the words file (words/{file_uid}/{run_id}.json): every heard word of the media file in order, as
// ai-trns's diarize job timed it (?format=json, CTC forced alignment); the immutable layer
type Words struct {
	Words []Word `json:"words"`
}

// DiarizeParagraph is a paragraph of ai-trns's diarize job (?format=paragraphs) without its text and words: who
// speaks (Speaker: "Rav", "Reader", SPEAKER_NN; Label: its display name), whether the source is read, when. A
// paragraph's words are those of the run's words between its start and the next one's.
type DiarizeParagraph struct {
	Speaker string  `json:"speaker"`
	Label   string  `json:"label"`
	Reading bool    `json:"reading"`
	Start   float64 `json:"start"`
	End     float64 `json:"end"`
}

// Diarization is a run's diarization as ai-trns gave it, without the text (table diarization)
type Diarization struct {
	Paragraphs []DiarizeParagraph `json:"paragraphs"`
}

// DiarizeAnswer is ai-trns's ?format=paragraphs answer
type DiarizeAnswer struct {
	Paragraphs []struct {
		DiarizeParagraph
		Words []Word `json:"words"`
	} `json:"paragraphs"`
	Stats map[string]interface{} `json:"stats"`
}

// Diarization is the answer without the text and the words
func (a *DiarizeAnswer) Diarization() *Diarization {
	d := &Diarization{Paragraphs: make([]DiarizeParagraph, len(a.Paragraphs))}
	for i, p := range a.Paragraphs {
		d.Paragraphs[i] = p.DiarizeParagraph
	}
	return d
}

// Document is the markup document served (GET /cus/{uid}/markup)
type Document struct {
	Version          int                    `json:"version"`
	UID              string                 `json:"uid"`
	OriginalLanguage string                 `json:"original_language"`
	Language         string                 `json:"language"`
	Media            Media                  `json:"media"`
	Transcript       *FileRef               `json:"transcript"`
	Run              DocumentRun            `json:"run"`
	Segmentation     DocumentSegmentation   `json:"segmentation"`
	Paragraphs       []Paragraph            `json:"paragraphs"`
	Chapters         []Chapter              `json:"chapters"`
	Stats            map[string]interface{} `json:"stats"`
}

type DocumentRun struct {
	ID          string `json:"id"`
	Created     string `json:"created"`
	WordsSource string `json:"words_source"`
	Source      string `json:"source"`
}

type DocumentSegmentation struct {
	Rev     int    `json:"rev"`
	Source  string `json:"source"`
	Created string `json:"created"`
}
