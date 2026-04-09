-- +goose Up
ALTER TABLE users ADD COLUMN auth_provider TEXT NOT NULL DEFAULT 'local';

-- +goose Down
ALTER TABLE users DROP COLUMN auth_provider;
