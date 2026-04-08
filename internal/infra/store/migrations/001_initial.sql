-- +goose Up

-- Users
CREATE TABLE IF NOT EXISTS users (
    id            TEXT PRIMARY KEY,
    email         TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    role          TEXT NOT NULL DEFAULT 'viewer'
                  CHECK (role IN ('admin', 'operator', 'viewer')),
    created_at    TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at    TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_login_at TEXT
);

-- Sessions
CREATE TABLE IF NOT EXISTS sessions (
    id         TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role       TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_sessions_expires ON sessions(expires_at);
CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id);

-- API Keys
CREATE TABLE IF NOT EXISTS api_keys (
    id           TEXT PRIMARY KEY,
    name         TEXT NOT NULL,
    hashed_key   TEXT NOT NULL UNIQUE,
    role         TEXT NOT NULL CHECK (role IN ('admin', 'operator', 'viewer')),
    created_by   TEXT NOT NULL REFERENCES users(id),
    last_used_at TEXT,
    expires_at   TEXT,
    created_at   TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_api_keys_hash ON api_keys(hashed_key);

-- Stacks
CREATE TABLE IF NOT EXISTS stacks (
    name       TEXT PRIMARY KEY,
    path       TEXT NOT NULL UNIQUE,
    source     TEXT NOT NULL DEFAULT 'local'
               CHECK (source IN ('local', 'git')),
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Git configuration for git-backed stacks
CREATE TABLE IF NOT EXISTS stack_git_configs (
    stack_name   TEXT PRIMARY KEY REFERENCES stacks(name) ON DELETE CASCADE,
    repo_url     TEXT NOT NULL,
    branch       TEXT NOT NULL DEFAULT 'main',
    compose_path TEXT NOT NULL DEFAULT 'compose.yaml',
    auto_sync    INTEGER NOT NULL DEFAULT 1,
    auth_method  TEXT NOT NULL DEFAULT 'none'
                 CHECK (auth_method IN ('none', 'token', 'ssh_key', 'basic')),
    credentials  TEXT,
    last_sync_at TEXT,
    last_commit  TEXT,
    sync_status  TEXT NOT NULL DEFAULT 'synced'
                 CHECK (sync_status IN ('synced', 'behind', 'diverged', 'error', 'syncing'))
);

-- Webhooks
CREATE TABLE IF NOT EXISTS webhooks (
    id            TEXT PRIMARY KEY,
    stack_name    TEXT NOT NULL REFERENCES stacks(name) ON DELETE CASCADE,
    provider      TEXT NOT NULL DEFAULT 'generic'
                  CHECK (provider IN ('github', 'gitlab', 'gitea', 'bitbucket', 'generic')),
    secret        TEXT NOT NULL,
    branch_filter TEXT,
    auto_redeploy INTEGER NOT NULL DEFAULT 1,
    events        TEXT,
    created_by    TEXT NOT NULL REFERENCES users(id),
    created_at    TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_webhooks_stack ON webhooks(stack_name);

-- Webhook deliveries
CREATE TABLE IF NOT EXISTS webhook_deliveries (
    id           TEXT PRIMARY KEY,
    webhook_id   TEXT NOT NULL REFERENCES webhooks(id) ON DELETE CASCADE,
    event        TEXT NOT NULL,
    branch       TEXT,
    commit_sha   TEXT,
    status       TEXT NOT NULL DEFAULT 'received'
                 CHECK (status IN ('received', 'processing', 'success', 'failed', 'skipped')),
    action       TEXT,
    error        TEXT,
    payload      TEXT,
    processed_at TEXT,
    created_at   TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_deliveries_webhook ON webhook_deliveries(webhook_id, created_at DESC);

-- Pipelines
CREATE TABLE IF NOT EXISTS pipelines (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    description TEXT,
    config      TEXT NOT NULL,
    created_by  TEXT NOT NULL REFERENCES users(id),
    created_at  TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Pipeline runs
CREATE TABLE IF NOT EXISTS pipeline_runs (
    id           TEXT PRIMARY KEY,
    pipeline_id  TEXT NOT NULL REFERENCES pipelines(id) ON DELETE CASCADE,
    status       TEXT NOT NULL DEFAULT 'pending'
                 CHECK (status IN ('pending', 'running', 'success', 'failed', 'cancelled')),
    triggered_by TEXT NOT NULL,
    started_at   TEXT,
    finished_at  TEXT,
    created_at   TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_runs_pipeline ON pipeline_runs(pipeline_id, created_at DESC);

-- Pipeline step results
CREATE TABLE IF NOT EXISTS pipeline_step_results (
    id          TEXT PRIMARY KEY,
    run_id      TEXT NOT NULL REFERENCES pipeline_runs(id) ON DELETE CASCADE,
    step_id     TEXT NOT NULL,
    step_name   TEXT NOT NULL,
    status      TEXT NOT NULL
                CHECK (status IN ('pending', 'running', 'success', 'failed', 'skipped')),
    output      TEXT,
    error       TEXT,
    duration_ms INTEGER,
    started_at  TEXT,
    finished_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_step_results_run ON pipeline_step_results(run_id);

-- Audit log
CREATE TABLE IF NOT EXISTS audit_log (
    id         TEXT PRIMARY KEY,
    user_id    TEXT,
    action     TEXT NOT NULL,
    resource   TEXT NOT NULL,
    detail     TEXT,
    ip_address TEXT,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_audit_created ON audit_log(created_at DESC);

-- Settings
CREATE TABLE IF NOT EXISTS settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- +goose Down

DROP TABLE IF EXISTS settings;
DROP TABLE IF EXISTS audit_log;
DROP TABLE IF EXISTS pipeline_step_results;
DROP TABLE IF EXISTS pipeline_runs;
DROP TABLE IF EXISTS pipelines;
DROP TABLE IF EXISTS webhook_deliveries;
DROP TABLE IF EXISTS webhooks;
DROP TABLE IF EXISTS stack_git_configs;
DROP TABLE IF EXISTS stacks;
DROP TABLE IF EXISTS api_keys;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS users;
