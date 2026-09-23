-- +goose Up
ALTER TABLE alerts ALTER COLUMN target_id DROP NOT NULL;

-- +goose Down
DELETE FROM alerts WHERE target_id IS NULL;
ALTER TABLE alerts ALTER COLUMN target_id SET NOT NULL;
