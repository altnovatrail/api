-- A problem report on the tutor's answer to the learner's question on a task from paper, whose
-- words the report keeps as app showed them, since api keeps none of them.
-- contract: the release before this one writes no report on an answer to a question.

-- +goose Up

ALTER TABLE problem_reports
    DROP CONSTRAINT problem_reports_reply_check,
    ADD CONSTRAINT problem_reports_reply_check
        CHECK (reply IN ('rung', 'explained_differently', 'correct_answer', 'essay_feedback', 'opening', 'answer'));
