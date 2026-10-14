-- +goose Up
ALTER TABLE alerts ADD status TEXT NOT NULL DEFAULT 'not_sent';
UPDATE alerts SET status = 'sent' WHERE notificated;
ALTER TABLE alerts ADD CONSTRAINT alerts_status_check CHECK (status IN ('not_sent','sent','cooldown'));
ALTER TABLE alerts DROP COLUMN notificated;
-- +goose Down
ALTER TABLE alerts ADD notificated bool NOT NULL DEFAULT FALSE;
UPDATE alerts SET notificated = true WHERE status <> 'not_sent';
ALTER TABLE alerts DROP COLUMN status;
