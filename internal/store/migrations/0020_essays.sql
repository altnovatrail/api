-- How a submission of an essay ended (R14, R15): "feedback", "unreadable" or
-- "unchecked", and beside feedback the mark of each criterion of the rubric, by its number in
-- the rubric from 1 — "met", "not_met" or "not_assessed" — and the source of the words: the
-- model's, or the phrase "no feedback this time" when they failed a check. The answer of such a
-- submission is the contract's SubmitEssayResponse without the text read and the words of the
-- feedback, which are nowhere on the server; an essay keeps no observation. A submission with a
-- verdict has none of the three.

-- +goose Up

ALTER TABLE submissions
    ADD COLUMN essay_end text CHECK (essay_end IN ('feedback', 'unreadable', 'unchecked')),
    ADD COLUMN marks text[] CHECK (cardinality(marks) >= 1
        AND marks <@ ARRAY['met', 'not_met', 'not_assessed']::text[]),
    ADD COLUMN words_source text CHECK (words_source IN ('model', 'no_feedback')),
    ADD CHECK (num_nonnulls(marks, words_source) = CASE WHEN essay_end = 'feedback' THEN 2 ELSE 0 END),
    ADD CHECK (num_nulls(essay_end, observation) >= 1);
