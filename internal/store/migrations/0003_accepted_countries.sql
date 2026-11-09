-- The countries a deployment takes learners from before the DPIA (П-2, П-6; R36). None
-- at first: closed by default. The operator's command api countries keeps the list,
-- and refuses to open a country of the EU, the EEA or the United Kingdom.

-- +goose Up

CREATE TABLE accepted_countries (
    country text PRIMARY KEY CHECK (country ~ '^[A-Z]{2}$'),
    added_at timestamptz NOT NULL DEFAULT now()
);
