-- Runs: the lesson's media file -> ai-trns words job -> ai-trns speakers job -> words kept, segmentation rev auto
CREATE TABLE IF NOT EXISTS run (
    id            TEXT PRIMARY KEY,
    cu_uid        TEXT NOT NULL,
    kind          TEXT NOT NULL,                -- first | file | model
    language      TEXT NOT NULL,
    file_uid      TEXT NOT NULL,                -- the media file transcribed
    stage         TEXT NOT NULL,                -- words -> speakers
    status        TEXT NOT NULL,                -- pending -> ready | failed
    words_source  TEXT NOT NULL,                -- stt | vtt
    vtt_file_uid  TEXT,
    error         TEXT,
    jobs          TEXT NOT NULL DEFAULT '{}',   -- ai-trns jobs {words, speakers}
    data          TEXT NOT NULL DEFAULT '{}',   -- audio_url, original_language, media, transcript, finished, stats
    author        TEXT,
    created       TEXT NOT NULL,
    updated       TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS run_cu ON run (cu_uid, created);
CREATE INDEX IF NOT EXISTS run_status ON run (status);

-- The lesson's segmentation (paragraphs, speakers, chapters) by revision; the current one is computed
CREATE TABLE IF NOT EXISTS segmentation (
    cu_uid        TEXT NOT NULL,
    rev           INTEGER NOT NULL,
    file_uid      TEXT NOT NULL,
    words_run_id  TEXT NOT NULL,
    source        TEXT NOT NULL,                -- auto | human | migrated
    based_on      INTEGER,
    author        TEXT,
    created       TEXT NOT NULL,
    data          TEXT NOT NULL,                -- {source: transcript | audio, paragraphs, chapters, stats}
    PRIMARY KEY (cu_uid, rev)
);
CREATE INDEX IF NOT EXISTS segmentation_run ON segmentation (words_run_id);
