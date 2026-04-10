-- +goose Up
-- Performance indexes (P14)
CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions(expires_at);
CREATE INDEX IF NOT EXISTS idx_api_keys_created_by ON api_keys(created_by);
CREATE INDEX IF NOT EXISTS idx_stacks_source ON stacks(source);
CREATE INDEX IF NOT EXISTS idx_stack_git_configs_stack_name ON stack_git_configs(stack_name);
CREATE INDEX IF NOT EXISTS idx_webhooks_stack_name ON webhooks(stack_name);
CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_webhook_id ON webhook_deliveries(webhook_id);
CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_created_at ON webhook_deliveries(created_at);
CREATE INDEX IF NOT EXISTS idx_pipeline_runs_pipeline_id ON pipeline_runs(pipeline_id);
CREATE INDEX IF NOT EXISTS idx_pipeline_runs_started_at ON pipeline_runs(started_at);
CREATE INDEX IF NOT EXISTS idx_audit_log_user_id ON audit_log(user_id);
CREATE INDEX IF NOT EXISTS idx_audit_log_created_at ON audit_log(created_at);

-- +goose Down
DROP INDEX IF EXISTS idx_sessions_user_id;
DROP INDEX IF EXISTS idx_sessions_expires_at;
DROP INDEX IF EXISTS idx_api_keys_created_by;
DROP INDEX IF EXISTS idx_stacks_source;
DROP INDEX IF EXISTS idx_stack_git_configs_stack_name;
DROP INDEX IF EXISTS idx_webhooks_stack_name;
DROP INDEX IF EXISTS idx_webhook_deliveries_webhook_id;
DROP INDEX IF EXISTS idx_webhook_deliveries_created_at;
DROP INDEX IF EXISTS idx_pipeline_runs_pipeline_id;
DROP INDEX IF EXISTS idx_pipeline_runs_started_at;
DROP INDEX IF EXISTS idx_audit_log_user_id;
DROP INDEX IF EXISTS idx_audit_log_created_at;
