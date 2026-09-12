-- +goose Up
SELECT 'up SQL query';

CREATE TABLE IF NOT EXISTS targets (
    id uuid PRIMARY KEY,
    source text NOT NULL,
    name text NOT NULL,
    type text NOT NULL,
    status text NOT NULL DEFAULT 'unknown',
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS events (
    id SERIAL PRIMARY KEY,
    target_id uuid NOT NULL,
    event int NOT NULL CHECK (event BETWEEN 0 AND 4),
    details text,
    event_time timestamptz NOT NULL DEFAULT NOW(),
    FOREIGN KEY (target_id) REFERENCES targets(id)
);

CREATE UNIQUE INDEX IF NOT EXISTS events_target_event_time_idx
    ON events (target_id, event, event_time);

CREATE TABLE IF NOT EXISTS log_matches (
    id SERIAL PRIMARY KEY,
    target_id uuid NOT NULL,
    severity int NOT NULL CHECK (severity BETWEEN 0 AND 4),
    message jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT NOW(),
    FOREIGN KEY (target_id) REFERENCES targets(id)
);

CREATE TABLE IF NOT EXISTS alerts (
    id SERIAL PRIMARY KEY,
    target_id uuid NOT NULL,
    severity int NOT NULL CHECK (severity BETWEEN 0 AND 4),
    details text NOT NULL,
    notificated boolean NOT NULL DEFAULT FALSE,
    created_at timestamptz NOT NULL DEFAULT NOW(),
    FOREIGN KEY (target_id) REFERENCES targets(id)
);

CREATE TABLE IF NOT EXISTS probes (
    id SERIAL PRIMARY KEY,
    target_id uuid NOT NULL,
    success boolean NOT NULL DEFAULT FALSE,
    latency integer NOT NULL,
    created_at timestamptz NOT NULL DEFAULT NOW(),
    FOREIGN KEY (target_id) REFERENCES targets(id)
);

CREATE INDEX ON log_matches (target_id);
CREATE INDEX ON alerts (target_id);
CREATE INDEX ON probes (target_id);

-- +goose Down
SELECT 'down SQL query';

DROP TABLE targets CASCADE;
DROP TABLE events CASCADE;
DROP TABLE log_matches CASCADE;
DROP TABLE alerts CASCADE;
DROP TABLE probes CASCADE;