-- +goose Up
-- Singleton system_upgrade table for self-upgrade orchestration.
-- Only one row may exist at a time (enforced by application-level upsert).
CREATE TABLE IF NOT EXISTS system_upgrade (
    id              INTEGER PRIMARY KEY CHECK (id = 1),
    status          TEXT NOT NULL DEFAULT 'pending'
                    CHECK (status IN ('pending', 'helper_running', 'completed', 'failed')),
    helper_id       TEXT,
    started_by      TEXT NOT NULL DEFAULT '',
    from_version    TEXT NOT NULL,
    target_image    TEXT NOT NULL,
    deployment_type TEXT NOT NULL DEFAULT 'unknown'
                    CHECK (deployment_type IN ('compose', 'docker_run', 'unknown')),
    error_message   TEXT,
    created_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- +goose Down
DROP TABLE IF EXISTS system_upgrade;
