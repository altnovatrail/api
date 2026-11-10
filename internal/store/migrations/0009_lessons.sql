-- What a lesson run needs of the rest (R48; R13; architecture 3.2, 3.7): the lesson an
-- engine's choice of a lesson names, and the end of a run whose attempt of a segment is
-- closed "unchecked".
--
-- contract: the check of a choice is narrowed only for the choices of a lesson, which no
-- release before this one writes; everything else here only adds (R29).

-- +goose Up

-- The lesson an engine's choice of a lesson names: the module and the lesson's local ID. The
-- choice names no version: the run begins on the module's current version (R07). The rest of
-- a choice comes with the engine's code.
ALTER TABLE engine_choices
    ADD COLUMN lesson_id text CHECK (char_length(lesson_id) BETWEEN 1 AND 64),
    ADD CHECK (kind <> 'lesson' OR num_nonnulls(module_id, lesson_id) = 2);

-- An attempt of a segment closed "unchecked" — its version withdrawn or out of the window —
-- ends its run "unchecked" at the same time, in the same transaction, whichever way it closes:
-- the check of a request, the beginning of another attempt, or the job after a deployment
-- (R07, R13).
-- +goose StatementBegin
CREATE FUNCTION attempt_ends_run() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    UPDATE lesson_runs SET end_reason = 'unchecked', ended_at = NEW.closed_at
        WHERE profile_id = NEW.profile_id AND id = NEW.lesson_run_id AND ended_at IS NULL;
    RETURN NULL;
END
$$;
-- +goose StatementEnd

CREATE TRIGGER attempts_end_run AFTER UPDATE OF closed_at ON attempts FOR EACH ROW
    WHEN (OLD.closed_at IS NULL AND NEW.close_reason = 'unchecked' AND NEW.lesson_run_id IS NOT NULL)
    EXECUTE FUNCTION attempt_ends_run();
