-- The key of an attempt of a task from paper (R162 spec): the message Key of the package
-- novatrail.paper.v1, its provenance and its answer, which the first submission derives from the
-- task's statement and every later one judges by; a Key with no answer says the task has none. It
-- is kept while the attempt is open, and goes with its close, in the close's own transaction,
-- whichever way it closes (privacy 4.1 spec). It never reaches the device (R05 spec).
-- contract: the release before this one writes no key and reads none: it refuses a submission of
-- an attempt of paper, so the rows it leaves have none.

-- +goose Up

ALTER TABLE attempts
    ADD COLUMN paper_key bytea CHECK (paper_key IS NULL OR kind = 'paper'),
    ADD CONSTRAINT attempts_paper_key_open CHECK (paper_key IS NULL OR closed_at IS NULL);

-- +goose StatementBegin
CREATE FUNCTION attempt_paper_key_go() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    NEW.paper_key := NULL;
    RETURN NEW;
END
$$;
-- +goose StatementEnd

CREATE TRIGGER attempts_paper_key_go BEFORE UPDATE OF closed_at ON attempts FOR EACH ROW
    WHEN (OLD.closed_at IS NULL AND NEW.closed_at IS NOT NULL) EXECUTE FUNCTION attempt_paper_key_go();
