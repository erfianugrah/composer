-- +goose Up
-- Seeds a sentinel row in `stacks` for the reserved "_system" webhook scope.
-- webhooks.stack_name has REFERENCES stacks(name) (001_initial.sql), but
-- migration 006 never created a backing row for "_system" -- so creating
-- a _system webhook (the self-upgrade trigger) violated the FK and 500'd.
-- The self-upgrade webhook receiver (webhook.go: StackName == "_system")
-- never reads this row's path/source; it dispatches straight to the
-- upgrade service. StackRepo.GetByName/List hide this name from normal
-- stack listings and gets so it never surfaces as a phantom stack in the
-- UI or becomes reachable via the generic stack API.
-- path is a sentinel, not a real filesystem path (never resolved/read) --
-- guaranteed unique since it isn't a valid absolute path any real stack
-- would use.
INSERT OR IGNORE INTO stacks (name, path, source) VALUES ('_system', '::system-reserved::', 'local');

-- +goose Down
DELETE FROM stacks WHERE name = '_system';
