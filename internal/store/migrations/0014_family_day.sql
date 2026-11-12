-- The beginning of the account's current family day, beside its end and its offset, which 0001
-- added (R11, R12; architecture 3.6): the counters of the quota are keyed by the beginning of their
-- day, and a first day by a new offset may begin at the end of the day before rather than at a
-- midnight, so the end alone does not give it.

-- +goose Up

ALTER TABLE accounts ADD COLUMN day_starts_at timestamptz;
