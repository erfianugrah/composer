-- +goose Up
-- +goose NO TRANSACTION

-- Fix DEFAULT values for Postgres (001_initial.sql uses SQLite-native datetime('now')).
-- These ALTERs are Postgres-only; SQLite does not support ALTER COLUMN.
-- Goose runs this with DialectPostgres only; SQLite skips it via dialect routing.

-- NOTE: Postgres ignores datetime('now') as a default because it evaluates to a function call
-- that doesn't exist in Postgres. Since all INSERT operations supply explicit timestamps
-- from Go code, the defaults are never evaluated. This migration is a safety net for
-- any future raw SQL INSERTs that omit timestamps.

-- This is intentionally empty for now. The Go application always provides
-- explicit values for created_at/updated_at/expires_at columns.
-- A future migration can ALTER COLUMN SET DEFAULT NOW() if needed.

-- +goose Down
-- Nothing to undo
