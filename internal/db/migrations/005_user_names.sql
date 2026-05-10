-- +goose Up
-- 005_user_names.sql
--
-- Adds first_name and last_name columns to users. SQLite supports
-- ADD COLUMN with NOT NULL when a DEFAULT is supplied, so a full table
-- recreate is not necessary here.
--
-- Existing rows get the empty-string default; the application enforces
-- non-empty values at the boundary on new inserts.

ALTER TABLE users ADD COLUMN first_name TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN last_name  TEXT NOT NULL DEFAULT '';
