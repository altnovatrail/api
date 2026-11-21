-- The right of the debug mode of the models of a task from paper, the account's (R163 spec; R97):
-- DebugService and the debug actions of a request serve an account whose row has it, and
-- `api debug-access` gives it and takes it by the account's UID. Every account there is when this
-- runs gets it, but one marked "deleting", whose fields are cleared (R21); an account made later
-- has it off. The deletion clears it with the account's other fields.

-- +goose Up

ALTER TABLE accounts ADD COLUMN debug_models boolean NOT NULL DEFAULT false;

UPDATE accounts SET debug_models = true WHERE deleting_at IS NULL;
