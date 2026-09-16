-- +goose Up
CREATE TEMP TABLE target_dupes ON COMMIT DROP AS
SELECT t.id AS dup_id, c.id AS keep_id
FROM targets t
JOIN (
    SELECT DISTINCT ON (source, type) id, source, type
    FROM targets
    ORDER BY source, type, created_at, id
) c ON c.source = t.source AND c.type = t.type
WHERE t.id <> c.id;

DELETE FROM events e
USING target_dupes d
WHERE e.target_id = d.dup_id
  AND EXISTS (
      SELECT 1
      FROM events k
      WHERE k.target_id = d.keep_id
        AND k.event = e.event
        AND k.event_time = e.event_time
  );

UPDATE events e SET target_id = d.keep_id FROM target_dupes d WHERE e.target_id = d.dup_id;
UPDATE log_matches l SET target_id = d.keep_id FROM target_dupes d WHERE l.target_id = d.dup_id;
UPDATE alerts a SET target_id = d.keep_id FROM target_dupes d WHERE a.target_id = d.dup_id;
UPDATE probes p SET target_id = d.keep_id FROM target_dupes d WHERE p.target_id = d.dup_id;

DELETE FROM targets t USING target_dupes d WHERE t.id = d.dup_id;

CREATE UNIQUE INDEX IF NOT EXISTS targets_source_type_idx ON targets (source, type);

-- +goose Down
DROP INDEX IF EXISTS targets_source_type_idx;
