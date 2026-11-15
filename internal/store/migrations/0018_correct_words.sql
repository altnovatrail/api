-- Whether the tutor's words after a correct answer were given on the attempt (R09; architecture
-- 3.5): they are given once, so the second request gets "no more words", and the export says so,
-- correct_answer_words_given. The words themselves are kept nowhere. Only an attempt a correct
-- answer closed has them.

-- +goose Up

ALTER TABLE attempts
    ADD COLUMN correct_words_given boolean NOT NULL DEFAULT false,
    ADD CHECK (NOT correct_words_given OR coalesce(close_reason = 'correct', false));
