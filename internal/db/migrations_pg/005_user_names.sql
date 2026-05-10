-- +goose Up
-- 005_user_names.sql (PostgreSQL)
--
-- Adds first_name and last_name columns to users. Existing rows receive
-- the empty-string default; the application layer enforces non-empty
-- values for new inserts.

ALTER TABLE users ADD COLUMN first_name TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN last_name  TEXT NOT NULL DEFAULT '';
