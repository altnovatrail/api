-- Grown-up mode on an attempt of a module: the time it first acted on the attempt, the one thing
-- kept of a grown-up's request, by which the attempt owes no evidence. And what no screen has any
-- more: the solving mode and "ask before showing" of a profile, which a new profile no longer
-- writes and so take their defaults here, and the techniques given at a solving step. Their values
-- are set to the defaults, and the times of a step cleared, so that nothing of them is kept: the
-- columns go once no release serving reads them.

-- +goose Up

ALTER TABLE attempts
    ADD COLUMN grown_up_at timestamptz CHECK (grown_up_at IS NULL OR kind = 'module');

ALTER TABLE profiles
    ALTER COLUMN default_mode SET DEFAULT 'by_steps',
    ALTER COLUMN ask_before_showing SET DEFAULT false;

UPDATE profiles SET default_mode = 'by_steps', ask_before_showing = false
    WHERE default_mode <> 'by_steps' OR ask_before_showing;

UPDATE attempts SET step_techniques_at = NULL WHERE step_techniques_at IS NOT NULL;
