-- A run's diarization: ai-trns's paragraphs (?format=paragraphs) without the text and the words -- who speaks,
-- reading or not, when. Immutable, one per run; the input of a rebuild together with the run's words file
CREATE TABLE IF NOT EXISTS diarization (
    run_id        TEXT PRIMARY KEY REFERENCES run (id),
    data          TEXT NOT NULL                 -- {paragraphs: [{speaker, label, reading, start, end}]}
);
