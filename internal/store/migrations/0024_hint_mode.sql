-- The hint mode of a profile, "Show where" or "Questions only", which the learner and an adult set:
-- with "Show where", a task from paper gets the wavy line under the place of the first mistake with
-- the words of a rung. A new profile and every profile there is have "Show where". And the opening
-- question of a task from paper, the tutor's words before its first wrong answer, kept as the words
-- of the rung 0 of its attempt, the model's alone, and gone with its close as a rung's words are,
-- on which an adult may report a problem.
-- contract: the release before this one writes rungs 1 to 3 alone, reads no rung 0, writes no
-- report on an opening question, and reads and writes no hint mode, which stays at its default.

-- +goose Up

ALTER TABLE profiles
    ADD COLUMN hint_mode text NOT NULL DEFAULT 'show_where' CHECK (hint_mode IN ('questions_only', 'show_where'));

ALTER TABLE attempt_rungs
    DROP CONSTRAINT attempt_rungs_rung_check,
    ADD CONSTRAINT attempt_rungs_rung_check CHECK (rung BETWEEN 0 AND 3),
    ADD CONSTRAINT attempt_rungs_opening CHECK (rung > 0 OR source = 'model' AND explained_at IS NULL);

ALTER TABLE problem_reports
    DROP CONSTRAINT problem_reports_reply_check,
    ADD CONSTRAINT problem_reports_reply_check
        CHECK (reply IN ('rung', 'explained_differently', 'correct_answer', 'essay_feedback', 'opening'));
