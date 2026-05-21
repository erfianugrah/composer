-- +goose Up
-- registry_credentials stores Docker registry auth entries.
--
-- stack_name == '' marks a GLOBAL credential, applied to every stack.
-- stack_name != '' marks a per-stack override: when a stack deploys, its
-- per-stack credentials win over global ones for the same registry. Multiple
-- rows with the same stack scope let composer authenticate against multiple
-- registries in one deploy (e.g. ghcr.io + a private mirror).
--
-- secret_enc is the registry password / PAT encrypted with AES-256-GCM via
-- internal/infra/crypto (same scheme as git credentials).
CREATE TABLE IF NOT EXISTS registry_credentials (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    registry   TEXT NOT NULL,
    username   TEXT NOT NULL,
    secret_enc TEXT NOT NULL,
    email      TEXT NOT NULL DEFAULT '',
    stack_name TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(registry, stack_name)
);

CREATE INDEX IF NOT EXISTS idx_registry_credentials_stack_name
    ON registry_credentials(stack_name);

-- +goose Down
DROP INDEX IF EXISTS idx_registry_credentials_stack_name;
DROP TABLE IF EXISTS registry_credentials;
