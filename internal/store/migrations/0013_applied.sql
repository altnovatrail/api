-- The age of digital consent a consent for a child was given by, for the consents written before
-- the table of ages (R52): the code before it took 13 for every country it accepted, so a category
-- it gave was given by 13. Without it, a learner of 13 to 17 of that code would be asked the age
-- again by every country (consent-age/README.md of spec, section 6). The release before this one
-- writes such a consent while a deployment goes on, and the trigger gives it the same.

-- +goose Up

UPDATE consents SET applied = 13 WHERE subject = 'child' AND applied IS NULL;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION consent_defaults() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.giver IS NULL THEN
        NEW.giver := CASE WHEN NEW.subject = 'adult' THEN 'owner' ELSE 'parent' END;
    END IF;
    IF NEW.giver = 'parent' AND NEW.form_state IS NULL THEN
        NEW.form_state := 'not_sent';
    END IF;
    IF NEW.giver = 'parent' AND NEW.request_code IS NULL THEN
        NEW.request_code := upper(substr(md5(random()::text), 1, 5)) || '-' || upper(substr(md5(random()::text), 1, 5));
    END IF;
    IF NEW.subject = 'child' AND NEW.applied IS NULL THEN
        NEW.applied := 13;
    END IF;
    RETURN NEW;
END
$$;
-- +goose StatementEnd
