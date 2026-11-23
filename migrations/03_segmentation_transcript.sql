-- A segmentation revision names the transcript it is built on (data.transcript); a run no longer does (it is
-- taken when the segmentation is built). Revisions written before: the transcript from their run's data.
UPDATE segmentation
SET data = json_set(data, '$.transcript',
                    json((SELECT json_extract(r.data, '$.transcript') FROM run r WHERE r.id = segmentation.words_run_id)))
WHERE json_extract(data, '$.source') = 'transcript'
  AND json_extract(data, '$.transcript') IS NULL
  AND (SELECT json_extract(r.data, '$.transcript.file_uid') FROM run r WHERE r.id = segmentation.words_run_id) IS NOT NULL;

UPDATE run SET data = json_remove(data, '$.transcript') WHERE json_type(data, '$.transcript') IS NOT NULL;
