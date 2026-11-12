-- Beside a choice of nothing to give, why the AI that checks the tasks was closed to the learner,
-- when it left a task out of the pool: not admitted, the quota of the family day — with when the
-- next one begins — or the limit of platform AI (R11, R59; contract, EngineChoice). Its answer
-- carried that outcome, and a repeat of the request that gave it gives it again. not_admitted stays,
-- set by any of the three, for the release still serving while this one rolls out, which reads it
-- alone; a choice it wrote has no cause, and reads as "not admitted".

-- +goose Up

ALTER TABLE engine_choices
    ADD COLUMN nothing_cause text CHECK (nothing_cause IN ('not_admitted', 'quota', 'platform_limit')),
    ADD COLUMN next_family_day timestamptz;
