-- The page of an attempt of a task from paper, by the ID app gives it: Start closes the open
-- attempts of another page alone, so that a Start again after "Add" leaves the page's own tasks
-- open. Kept while the attempt is open, and cleared by its close, as what 0025 clears.
-- contract: the release before this one writes no page and closes every open attempt of paper at a
-- Start.

-- +goose Up

ALTER TABLE attempts
    ADD COLUMN paper_page text CHECK (paper_page IS NULL OR kind = 'paper');

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION attempt_paper_count_go() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    NEW.wrong_answers := 0;
    NEW.paper_page := NULL;
    RETURN NEW;
END
$$;
-- +goose StatementEnd
