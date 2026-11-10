-- What an attempt without AI keeps (R44; architecture 3.2): the task an engine's choice of a
-- task names, the count of wrong answers and the techniques given at a step, and each
-- submission with a verdict, which a repeat under its idempotency key is answered by.
--
-- contract: the check of a record done is widened, not narrowed — the release before this one
-- writes every record done with its answer, which the wider check still takes; everything
-- else here only adds (R29).

-- +goose Up

-- The task an engine's choice of a task names: the module and the task's local ID. The choice
-- names no version: the attempt begins on the module's current version (R07). The rest of a
-- choice comes with the engine's code.
ALTER TABLE engine_choices
    ADD COLUMN module_id text CHECK (char_length(module_id) BETWEEN 1 AND 128),
    ADD COLUMN task_id text CHECK (char_length(task_id) BETWEEN 1 AND 128),
    ADD CHECK (kind <> 'task' OR num_nonnulls(module_id, task_id) = 2);

-- How many wrong answers the attempt has (R09), and when the techniques of its task were first
-- given at a step (R82 spec).
ALTER TABLE attempts
    ADD COLUMN wrong_answers integer NOT NULL DEFAULT 0 CHECK (wrong_answers >= 0),
    ADD COLUMN step_techniques_at timestamptz;

-- A submission with a verdict, by its place among the attempt's: its answer, the contract's
-- SubmitWorkResponse in binary protobuf without what comes next — the verdict of every item
-- and answer slot, what was read, the options tapped among it, the count of wrong answers, and
-- the task solved when it closed the attempt. It goes with its profile.
CREATE TABLE submissions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    profile_id uuid NOT NULL REFERENCES profiles (id) ON DELETE CASCADE,
    attempt_id uuid NOT NULL,
    number integer NOT NULL CHECK (number >= 1),
    submitted_at timestamptz NOT NULL DEFAULT now(),
    answer bytea NOT NULL,
    UNIQUE (attempt_id, number),
    UNIQUE (profile_id, id),
    FOREIGN KEY (profile_id, attempt_id) REFERENCES attempts (profile_id, id)
);

CREATE INDEX submissions_attempt ON submissions (profile_id, attempt_id);

-- A request done under an idempotency key holds its answer, or, when the answer holds what
-- the learner sent, the submission it recorded instead (architecture 3.2).
ALTER TABLE idempotency_keys
    ADD COLUMN submission_id uuid,
    ADD FOREIGN KEY (profile_id, submission_id) REFERENCES submissions (profile_id, id) ON DELETE CASCADE,
    DROP CONSTRAINT idempotency_keys_check,
    ADD CHECK ((state = 'done') = (response IS NOT NULL OR submission_id IS NOT NULL));

CREATE INDEX idempotency_keys_submission ON idempotency_keys (profile_id, submission_id) WHERE submission_id IS NOT NULL;
