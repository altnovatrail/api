-- A choice of nothing to give is written too (R50), of a kind of its own, so that what it holds —
-- the skill chosen on the skill map given back to the engine, the outcome its answer carried —
-- holds after it, and a repeat of the request that gave it gives it again.
--
-- contract: the check of the kind of a choice is dropped for one that widens it to nothing to
-- give; no release before this one writes a choice, and one that reads a choice of nothing as
-- neither a task nor a lesson refuses it as such (R29).

-- +goose Up

ALTER TABLE engine_choices
    DROP CONSTRAINT engine_choices_kind_check,
    ADD CHECK (kind IN ('task', 'lesson', 'nothing')),
    -- Beside nothing to give: whether AI that is not admitted left a task out of the pool, the
    -- outcome "not admitted" its answer carried.
    ADD COLUMN not_admitted boolean;
