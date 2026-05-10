-- +goose Up
-- 004_superadmin_role.sql (PostgreSQL)
--
-- Extends the user_type CHECK constraint to include the new 'superadmin' role.
-- PostgreSQL lets us drop and re-add the inline CHECK constraint by name.
-- The inline CHECK created in 002_auth_schema.sql receives the auto-generated
-- name users_user_type_check by PostgreSQL's naming convention.

ALTER TABLE users DROP CONSTRAINT IF EXISTS users_user_type_check;

ALTER TABLE users
    ADD CONSTRAINT users_user_type_check
    CHECK (user_type IN ('admin', 'user', 'superadmin'));
