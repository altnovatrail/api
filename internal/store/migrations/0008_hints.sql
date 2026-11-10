-- What the hint ladder keeps (R47; R09, R10; architecture 3.2): when a hint asked for first
-- gave the task's techniques and its leading question, and the words of each rung the attempt
-- reached — the tutor's question or the rung's fallback text, and the words of "explain
-- differently" — which live until the attempt closes. Once it has, each rung keeps its number
-- and where its words came from, and the words are gone (R05).
--
-- contract: everything here only adds (R29).

-- +goose Up

-- When a hint asked for first gave the task's techniques, and when it first gave its leading
-- question, which it records only when the task has one (R108 spec): two facts, kept apart, as
-- the evidence will carry them apart (the 2026-11-09 amendment to R82 spec).
ALTER TABLE attempts
    ADD COLUMN requested_techniques_at timestamptz,
    ADD COLUMN leading_question_at timestamptz;

-- The words of a rung of an attempt: where they came from, the model's question or the rung's
-- fallback text, and the words themselves; and, once "explain differently" was asked for on
-- the rung, when, where its words came from, and those words, which the learner reads from then
-- on. The words go with the attempt's close, and the rest stays.
CREATE TABLE attempt_rungs (
    profile_id uuid NOT NULL REFERENCES profiles (id) ON DELETE CASCADE,
    attempt_id uuid NOT NULL,
    rung integer NOT NULL CHECK (rung BETWEEN 1 AND 3),
    source text NOT NULL CHECK (source IN ('model', 'fallback')),
    words text CHECK (char_length(words) BETWEEN 1 AND 4000),
    explained_at timestamptz,
    explained_source text CHECK (explained_source IN ('model', 'fallback')),
    explained_words text CHECK (char_length(explained_words) BETWEEN 1 AND 4000),
    PRIMARY KEY (attempt_id, rung),
    FOREIGN KEY (profile_id, attempt_id) REFERENCES attempts (profile_id, id) ON DELETE CASCADE,
    CHECK ((explained_at IS NULL) = (explained_source IS NULL)),
    CHECK (explained_at IS NOT NULL OR explained_words IS NULL)
);

CREATE INDEX attempt_rungs_profile ON attempt_rungs (profile_id, attempt_id);

-- The close of an attempt deletes the words of its rungs, in its own transaction, whichever way
-- it closes — a correct answer, the solution shown, a new attempt, the term or a package gone,
-- and the daily job's close (R05, R10).
-- +goose StatementBegin
CREATE FUNCTION attempt_words_go() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    UPDATE attempt_rungs SET words = NULL, explained_words = NULL
        WHERE profile_id = NEW.profile_id AND attempt_id = NEW.id;
    RETURN NULL;
END
$$;
-- +goose StatementEnd

CREATE TRIGGER attempts_words_go AFTER UPDATE OF closed_at ON attempts FOR EACH ROW
    WHEN (OLD.closed_at IS NULL AND NEW.closed_at IS NOT NULL) EXECUTE FUNCTION attempt_words_go();
