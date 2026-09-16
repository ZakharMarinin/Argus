-- +goose Up
ALTER TABLE events DROP CONSTRAINT IF EXISTS events_target_id_fkey;
ALTER TABLE events ADD CONSTRAINT events_target_id_fkey
    FOREIGN KEY (target_id) REFERENCES targets(id) ON DELETE CASCADE;

ALTER TABLE log_matches DROP CONSTRAINT IF EXISTS log_matches_target_id_fkey;
ALTER TABLE log_matches ADD CONSTRAINT log_matches_target_id_fkey
    FOREIGN KEY (target_id) REFERENCES targets(id) ON DELETE CASCADE;

ALTER TABLE alerts DROP CONSTRAINT IF EXISTS alerts_target_id_fkey;
ALTER TABLE alerts ADD CONSTRAINT alerts_target_id_fkey
    FOREIGN KEY (target_id) REFERENCES targets(id) ON DELETE CASCADE;

ALTER TABLE probes DROP CONSTRAINT IF EXISTS probes_target_id_fkey;
ALTER TABLE probes ADD CONSTRAINT probes_target_id_fkey
    FOREIGN KEY (target_id) REFERENCES targets(id) ON DELETE CASCADE;

-- +goose Down
ALTER TABLE events DROP CONSTRAINT IF EXISTS events_target_id_fkey;
ALTER TABLE events ADD CONSTRAINT events_target_id_fkey
    FOREIGN KEY (target_id) REFERENCES targets(id);

ALTER TABLE log_matches DROP CONSTRAINT IF EXISTS log_matches_target_id_fkey;
ALTER TABLE log_matches ADD CONSTRAINT log_matches_target_id_fkey
    FOREIGN KEY (target_id) REFERENCES targets(id);

ALTER TABLE alerts DROP CONSTRAINT IF EXISTS alerts_target_id_fkey;
ALTER TABLE alerts ADD CONSTRAINT alerts_target_id_fkey
    FOREIGN KEY (target_id) REFERENCES targets(id);

ALTER TABLE probes DROP CONSTRAINT IF EXISTS probes_target_id_fkey;
ALTER TABLE probes ADD CONSTRAINT probes_target_id_fkey
    FOREIGN KEY (target_id) REFERENCES targets(id);
