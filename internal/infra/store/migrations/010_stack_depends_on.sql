-- +goose Up
-- depends_on is a JSON array of stack names that must deploy successfully
-- before this stack in a batch deploy (POST /api/v1/stacks/deploy-batch).
-- Motivation: on servarr every stack declares the servarr_lan network as
-- `external: true` but only the servarr stack creates it, so a parallel
-- "deploy all" after a reboot failed 18 stacks with "network declared as
-- external, but could not be found". Names, not FKs: a dependency that is
-- deleted is simply ignored at deploy time (DeployOrder drops edges whose
-- target is not in the batch), and TEXT + JSON is portable across SQLite
-- and Postgres. Numbered 010 because 008/009 are Go migrations
-- (migrations.go: docker_hosts, docker_host_certs).
ALTER TABLE stacks ADD COLUMN depends_on TEXT NOT NULL DEFAULT '[]';

-- +goose Down
ALTER TABLE stacks DROP COLUMN depends_on;
