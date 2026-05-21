-- +goose Up
-- env_path is the path to the .env file relative to the repo root for git-backed stacks.
-- Empty string means the legacy default ".env" at the repo root, so existing rows keep working.
ALTER TABLE stack_git_configs ADD COLUMN env_path TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE stack_git_configs DROP COLUMN env_path;
