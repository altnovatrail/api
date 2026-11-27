-- The task skill of a connected module: the skill the learner chose for the module's tasks, which
-- a choice of its tasks gives them on until the learner gives the choice back to the tutor; it goes
-- with the module's row when the module is disconnected. And the module a choice was among: of a
-- module's tasks alone, Solve on its card, or the module of a lesson the learner chose, whose
-- choice has the reason code 10, which no branch of the engine's rule gives.
-- contract: the release before this one writes no reason code 10, no task skill and no module of
-- a choice, and reads none of them: the check of the reason code only widens.

-- +goose Up

ALTER TABLE connected_modules
    ADD COLUMN task_skill text CHECK (char_length(task_skill) BETWEEN 1 AND 512);

ALTER TABLE engine_choices
    ADD COLUMN scope_module text CHECK (char_length(scope_module) BETWEEN 1 AND 128),
    DROP CONSTRAINT engine_choices_reason_code_check,
    ADD CONSTRAINT engine_choices_reason_code_check CHECK (reason_code BETWEEN 0 AND 10);
