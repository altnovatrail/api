-- What the close of an attempt of a task from paper leaves (R162 spec; R101): nothing of
-- what it kept while open but its row — its ID, its times and how it closed, which a problem report
-- on its tutor's words names (R123 spec). In the close's own transaction, whichever way it closes,
-- its submissions go, with what was read, their verdicts and the idempotency keys that recorded
-- them, its rungs, the opening question among them, and its count of wrong answers; its key goes by
-- migration 0023. The Check that solves it writes no submission, and its key's record keeps the
-- answer with `solved` alone, no verdict of a task (R101). An attempt of paper closed before this
-- migration loses them here. An attempt of a module keeps them, as the evidence and its repeat need.
-- contract: the release before this one keeps them after the close, and reads none of them of a
-- closed attempt of paper.

-- +goose Up
DELETE FROM submissions s USING attempts a
    WHERE a.profile_id = s.profile_id AND a.id = s.attempt_id AND a.kind = 'paper' AND a.closed_at IS NOT NULL;
DELETE FROM attempt_rungs r USING attempts a
    WHERE a.profile_id = r.profile_id AND a.id = r.attempt_id AND a.kind = 'paper' AND a.closed_at IS NOT NULL;
UPDATE attempts SET wrong_answers = 0 WHERE kind = 'paper' AND closed_at IS NOT NULL AND wrong_answers <> 0;

-- +goose StatementBegin
CREATE FUNCTION attempt_paper_count_go() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    NEW.wrong_answers := 0;
    RETURN NEW;
END
$$;
-- +goose StatementEnd

CREATE TRIGGER attempts_paper_count_go BEFORE UPDATE OF closed_at ON attempts FOR EACH ROW
    WHEN (OLD.closed_at IS NULL AND NEW.closed_at IS NOT NULL AND NEW.kind = 'paper')
    EXECUTE FUNCTION attempt_paper_count_go();

-- +goose StatementBegin
CREATE FUNCTION attempt_paper_rows_go() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    DELETE FROM submissions WHERE profile_id = NEW.profile_id AND attempt_id = NEW.id;
    DELETE FROM attempt_rungs WHERE profile_id = NEW.profile_id AND attempt_id = NEW.id;
    RETURN NULL;
END
$$;
-- +goose StatementEnd

CREATE TRIGGER attempts_paper_rows_go AFTER UPDATE OF closed_at ON attempts FOR EACH ROW
    WHEN (OLD.closed_at IS NULL AND NEW.closed_at IS NOT NULL AND NEW.kind = 'paper')
    EXECUTE FUNCTION attempt_paper_rows_go();
