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
	Source string `json:"source"` // transcript | audio
	// the kmedia transcript at the build: the paragraphs' I are its <p> if Source is transcript; its words are
	// aligned (Alignment) whatever the Source
	Transcript *FileRef               `json:"transcript,omitempty"`
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

// AlignedWord is a word of the transcript with its time on the media. Text: the token as written (punctuation
// kept). Match: "eq" the same heard word, "sub" another heard word in its place (its time), "" not heard -- its
// time shares the gap between its matched neighbours.
type AlignedWord struct {
	Text  string  `json:"t"`
	Start float64 `json:"s"`
	End   float64 `json:"e"`
	Match string  `json:"m,omitempty"`
}

// Alignment is the transcript's words on a run's words (align/{transcript_file_uid}/{run_id}.json): Paragraphs[p]
// are the whitespace tokens of the p-th <p> of its doc2html (empty for an empty <p>), in order. Derived: built
// again from the words and the transcript.
type Alignment struct {
	Paragraphs [][]AlignedWord `json:"paragraphs"`
	Stats      map[string]int  `json:"stats"` // words, eq, sub
}

// Timecode is the time of every word of the lesson's transcript (GET /cus/{uid}/timecode): Paragraphs[p][w] is
// the start of the w-th whitespace token of the p-th <p> of the transcript's doc2html, in seconds of the media
type Timecode struct {
	UID        string      `json:"uid"`
	Language   string      `json:"language"`
	Transcript FileRef     `json:"transcript"`
	Media      Media       `json:"media"`
	RunID      string      `json:"run_id"`
	Rev        int         `json:"rev"` // the segmentation revision it is taken from (the current one)
	Paragraphs [][]float64 `json:"paragraphs"`
}

// Located is the fragments of the lesson's transcript found (GET /cus/{uid}/locate), in the order asked
type Located struct {
	UID        string     `json:"uid"`
	Language   string     `json:"language"`
	Transcript FileRef    `json:"transcript"`
	RunID      string     `json:"run_id"`
	Rev        int        `json:"rev"`
	Results    []Fragment `json:"results"`
}

// Fragment is a fragment found: Found exact | fuzzy | "" (not found: the rest null); Start / End its time on the
// media, P / W its first word (the <p> of the transcript's doc2html and the place there, as the timecode),
// Chapter the chapter at its start
type Fragment struct {
	Q       string   `json:"q"`
	Found   string   `json:"found"`
	Start   *float64 `json:"start"`
	End     *float64 `json:"end"`
	P       *int     `json:"p"`
	W       *int     `json:"w"`
	Chapter *Chapter `json:"chapter"`
}

// TextWord is a word of the lesson's text with its time: of the transcript (P / W: its <p> and its place there,
// as the timecode) or, without one, heard (no P / W)
type TextWord struct {
	Text  string  `json:"t"`
	Start float64 `json:"s"`
	End   float64 `json:"e"`
	P     *int    `json:"p,omitempty"`
	W     *int    `json:"w,omitempty"`
}

// LessonText is the lesson's text at a moment or in a span of time (GET /cus/{uid}/text): its words (Source
// transcript: the transcript's; audio: the heard ones) and the paragraphs and chapters of the current revision
// there
type LessonText struct {
	UID        string      `json:"uid"`
	Language   string      `json:"language"`
	Rev        int         `json:"rev"`
	Source     string      `json:"source"` // transcript | audio
	Words      []TextWord  `json:"words"`
	Paragraphs []Paragraph `json:"paragraphs"`
	Chapters   []Chapter   `json:"chapters"`
}

// Phrase is a place of a phrase in the lesson: its time, how sure (Score: the share of the phrase's words there;
// 1 an exact match), the words around it, the paragraph and chapter there; P / W its first word in the transcript
// (Source transcript)
type Phrase struct {
	Start     float64    `json:"start"`
	End       float64    `json:"end"`
	Score     float64    `json:"score"`
	Source    string     `json:"source"` // transcript | audio: matched in the transcript or the heard words
	Context   string     `json:"context"`
	P         *int       `json:"p,omitempty"`
	W         *int       `json:"w,omitempty"`
	Paragraph *Paragraph `json:"paragraph"`
	Chapter   *Chapter   `json:"chapter"`
}

// Found is where a phrase is said in the lesson (GET /cus/{uid}/find), the best first
type Found struct {
	UID      string   `json:"uid"`
	Language string   `json:"language"`
	Rev      int      `json:"rev"`
	Q        string   `json:"q"`
	Results  []Phrase `json:"results"`
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
