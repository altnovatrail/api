-- The reading of the statement of an attempt of a task from paper (R169 spec): the message
-- Reading of the package novatrail.paper.v1, each task's statement and formula, which the
-- learner's "Recognise" makes and the first submission derives the key from with no call of its
-- own. It is kept while the attempt is open, and goes with its close, with the key of 0023, in the
-- close's own transaction, whichever way it closes. Its formulas never reach the device (R05 spec);
-- the export takes none of it.
-- contract: the release before this one writes no reading and reads none, and its close clears the
-- column by the trigger of 0023 this replaces.

-- +goose Up

ALTER TABLE attempts
    ADD COLUMN paper_reading bytea CHECK (paper_reading IS NULL OR kind = 'paper'),
    ADD CONSTRAINT attempts_paper_reading_open CHECK (paper_reading IS NULL OR closed_at IS NULL);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION attempt_paper_key_go() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    NEW.paper_key := NULL;
    NEW.paper_reading := NULL;
    RETURN NEW;
END
$$;
-- +goose StatementEnd
