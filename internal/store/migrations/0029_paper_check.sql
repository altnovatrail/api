-- The Check of an attempt of a task from paper: the message Check of the package
-- novatrail.paper.v1 of the last Check, which a model gave — each task's verdict, what it read and
-- where, the boxes of its errors, its hidden solution and answer, the tutor's words and the ladder
-- of hints —, which every Check writes in place of the one before; and the attempt's history, the
-- Checks before it, each a Check of its verdicts, its tutor's words and the rungs of its ladder the
-- learner was shown in its hints alone, which the next Check is given. Both are kept while the
-- attempt is open, and go with its close, with the key and the reading, by the trigger of 0023 in
-- the close's own transaction, whichever way it closes. Neither reaches the device, and the export
-- takes none of them. The key of 0023 is written no more: the release before this one still reads
-- it, so it stays until a release after this one drops it.
-- contract: the release before this one writes no Check and no history and reads neither, and its
-- close clears the key and the reading by the trigger this replaces.

-- +goose Up

ALTER TABLE attempts
    ADD COLUMN paper_check bytea CHECK (paper_check IS NULL OR kind = 'paper'),
    ADD COLUMN paper_history bytea[] CHECK (paper_history IS NULL OR kind = 'paper'),
    ADD CONSTRAINT attempts_paper_check_open CHECK (paper_check IS NULL OR closed_at IS NULL),
    ADD CONSTRAINT attempts_paper_history_open CHECK (paper_history IS NULL OR closed_at IS NULL);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION attempt_paper_key_go() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    NEW.paper_key := NULL;
    NEW.paper_reading := NULL;
    NEW.paper_check := NULL;
    NEW.paper_history := NULL;
    RETURN NEW;
END
$$;
-- +goose StatementEnd
