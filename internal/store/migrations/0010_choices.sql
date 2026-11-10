-- What an engine's choice keeps (R06, R50; R107 spec): its reason code, what it handed out, the
-- package it was made on, the input a replay needs, and the close or the end of a run whose
-- next it is. A choice written before this release keeps none of it.

-- +goose Up

ALTER TABLE engine_choices
    -- The reason code of the branch of the engine's rule that made it, ReasonCode's number.
    ADD COLUMN reason_code smallint CHECK (reason_code BETWEEN 0 AND 9),
    -- What it handed out, by full ID: the primary skills of the task and its items, or the
    -- lesson's skills, so that the summary of issues needs no package that left the image.
    ADD COLUMN issued_skills text[],
    -- The package of the module's current version it was made on.
    ADD COLUMN package_digest text CHECK (package_digest ~ '^sha256:[0-9a-f]{64}$'),
    -- The input a replay needs beyond the records: the skill chosen on the skill map, held until
    -- the learner asks for something else; the age category and the grade of the profile then;
    -- whether the pool was limited to the tasks a tap checks.
    ADD COLUMN map_skill text CHECK (char_length(map_skill) BETWEEN 1 AND 512),
    ADD COLUMN age_category text CHECK (age_category IN ('under_13', 'from_13_to_consent', 'from_consent_to_18', 'adult')),
    ADD COLUMN grade smallint CHECK (grade BETWEEN 1 AND 12),
    ADD COLUMN pool_limited boolean,
    -- The versions, the release and the window it was made on, and the number of the last
    -- evidence of the journal it read.
    ADD COLUMN engine_version text CHECK (char_length(engine_version) BETWEEN 1 AND 64),
    ADD COLUMN learner_model_version text CHECK (char_length(learner_model_version) BETWEEN 1 AND 64),
    ADD COLUMN modules_release text CHECK (char_length(modules_release) BETWEEN 1 AND 64),
    ADD COLUMN format_window text CHECK (char_length(format_window) BETWEEN 1 AND 16),
    ADD COLUMN last_seq bigint CHECK (last_seq >= 0),
    -- The digests of every graph the learner model and the engine got, and of those, the
    -- modules of the engine: connected and fit for the learner.
    ADD COLUMN digests text[],
    ADD COLUMN engine_digests text[],
    -- The close of an attempt, or the end of a lesson run, that gave it as what comes next: a
    -- repeat of that request finds it again. One of them at most.
    ADD COLUMN after_attempt_id uuid UNIQUE,
    ADD COLUMN after_lesson_run_id uuid UNIQUE,
    ADD FOREIGN KEY (profile_id, after_attempt_id) REFERENCES attempts (profile_id, id) ON DELETE CASCADE,
    ADD FOREIGN KEY (profile_id, after_lesson_run_id) REFERENCES lesson_runs (profile_id, id) ON DELETE CASCADE,
    ADD CHECK (num_nonnulls(after_attempt_id, after_lesson_run_id) <= 1);
