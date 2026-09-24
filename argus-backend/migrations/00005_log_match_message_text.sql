-- +goose Up
ALTER TABLE log_matches ALTER COLUMN message TYPE text USING message #>> '{}';

-- +goose Down
ALTER TABLE log_matches ALTER COLUMN message TYPE jsonb USING to_jsonb(message);
