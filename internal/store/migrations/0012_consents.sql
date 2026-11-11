-- The contract of consent of spec's epic E4 (R52; R116, R130, R134, R136 spec): who gives a
-- consent — the owner, a parent by a signed form, or the learner — the code of a parent's request,
-- the state of the form and where its scan lies, outside this database, the age of the country the
-- category was given by, and the versions of the texts a consent is given to; the texts an account
-- was created on; the storage of works; and the problem reports of an adult.
--
-- contract: the check that only a confirmed consent has a profile is dropped — a learner who
-- consents themselves has their profile at once, which waits for the consent, and a profile whose
-- age is given again waits for a new one. No release before this one writes a consent of a profile
-- that waits, and one that reads a profile's consent as confirmed or not reads such a consent as not
-- confirmed, which refuses its learning as no consent (R29).

-- +goose Up

ALTER TABLE consents
    DROP CONSTRAINT consents_check1,
    -- Who gives it: the owner of the account, of their own profile, by their statement; a parent,
    -- by the signed form of parent-consent; or the learner, from the age of digital consent of
    -- their country (R116 spec).
    ADD COLUMN giver text CHECK (giver IN ('owner', 'parent', 'learner')),
    -- A parent's: the code of the request the parent writes on the form, ten characters of
    -- Crockford's base32 in two groups of five, and the state of its form.
    ADD COLUMN request_code text CHECK (request_code ~ '^[0-9A-HJKMNP-TV-Z]{5}-[0-9A-HJKMNP-TV-Z]{5}$'),
    ADD COLUMN form_state text CHECK (form_state IN ('not_sent', 'sent', 'not_accepted')),
    -- The age of digital consent of the country on the day the age was given, which the category
    -- was given by (consent-age/README.md of spec, section 6).
    ADD COLUMN applied smallint CHECK (applied BETWEEN 13 AND 18),
    -- The version of the text the consent is given to: parent-consent of a parent's, teen-consent of
    -- the learner's; none while the text had no version in force.
    ADD COLUMN text_version integer CHECK (text_version >= 1),
    ADD CHECK ((giver = 'parent') = (request_code IS NOT NULL) OR giver IS NULL),
    ADD CHECK ((giver = 'parent') = (form_state IS NOT NULL) OR giver IS NULL);

UPDATE consents SET giver = CASE WHEN subject = 'adult' THEN 'owner' ELSE 'parent' END,
    form_state = CASE WHEN subject = 'child' THEN 'not_sent' END,
    request_code = CASE WHEN subject = 'child' THEN
        upper(substr(md5(random()::text), 1, 5)) || '-' ||
        upper(substr(md5(random()::text), 1, 5)) END
    WHERE giver IS NULL;

ALTER TABLE consents ALTER COLUMN giver SET NOT NULL;

-- The release before this one writes a consent with none of these fields while a deployment goes
-- on (R29): its owner's consent is the owner's, and its child's a parent's, which waits for a form
-- by a code of its own. This release always names them.
-- +goose StatementBegin
CREATE FUNCTION consent_defaults() RETURNS trigger LANGUAGE plpgsql AS $$
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
    RETURN NEW;
END
$$;
-- +goose StatementEnd

CREATE TRIGGER consents_defaults BEFORE INSERT ON consents FOR EACH ROW EXECUTE FUNCTION consent_defaults();

-- A code of a request is unique among the consents that wait (contract, ChildConsent).
CREATE UNIQUE INDEX consents_request_code ON consents (request_code) WHERE state = 'waiting';

-- The scans of the forms of a parent's consent, which lie in the closed bucket of the operator, out
-- of this database (R116 spec): where each lies, and until when it is kept. A scan is the consent's
-- while its consent is there; once the consent is gone, the scan of one confirmed is kept 5 years
-- after, as its minimal record is (R117 spec), and the scan of one that waited goes at once — the
-- daily job erases both by keep_until.
CREATE TABLE consent_forms (
    object text PRIMARY KEY CHECK (char_length(object) >= 1),
    consent_id uuid UNIQUE REFERENCES consents (id) ON DELETE SET NULL,
    media_type text NOT NULL CHECK (media_type IN ('image/jpeg', 'image/png', 'application/pdf')),
    sent_at timestamptz NOT NULL,
    keep_until date
);

-- +goose StatementBegin
CREATE FUNCTION consent_form_kept() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    UPDATE consent_forms SET keep_until = CASE WHEN OLD.state = 'confirmed'
            THEN ((now() AT TIME ZONE 'UTC')::date + interval '5 years')::date
            ELSE (now() AT TIME ZONE 'UTC')::date END
        WHERE consent_id = OLD.id;
    RETURN OLD;
END
$$;
-- +goose StatementEnd

CREATE TRIGGER consents_form_kept BEFORE DELETE ON consents FOR EACH ROW EXECUTE FUNCTION consent_form_kept();

-- The texts the owner accepted when the account was created: privacy-policy and terms-of-use, by
-- the versions the request named (contract, CreateAccount).
CREATE TABLE account_texts (
    account_id uuid NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
    text_id text NOT NULL CHECK (text_id ~ '^[a-z][a-z0-9]*(-[a-z0-9]+)*$'),
    version integer NOT NULL CHECK (version >= 1),
    accepted_at timestamptz NOT NULL,
    PRIMARY KEY (account_id, text_id)
);

-- The storage of a learner's works on the server (R113 spec): whether it is on, and the record of
-- each time it was switched on — who gave the consent, to which version of storage-consent — and
-- off. The record lives as long as the profile.
ALTER TABLE profiles ADD COLUMN work_storage boolean NOT NULL DEFAULT false;

CREATE TABLE work_storage_consents (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    profile_id uuid NOT NULL REFERENCES profiles (id) ON DELETE CASCADE,
    giver text NOT NULL CHECK (giver IN ('owner', 'parent', 'learner')),
    text_version integer CHECK (text_version >= 1),
    on_at timestamptz NOT NULL,
    off_at timestamptz,
    CHECK (off_at IS NULL OR off_at >= on_at)
);

CREATE INDEX work_storage_consents_profile ON work_storage_consents (profile_id, on_at);

-- The problem reports of an adult on a reply of the tutor (R123 spec): kept until the operator
-- reviews one and a year after; the deletion of the profile or the account keeps it to its term
-- without their IDs, nor the attempt's.
CREATE TABLE problem_reports (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id uuid REFERENCES accounts (id) ON DELETE SET NULL,
    profile_id uuid REFERENCES profiles (id) ON DELETE SET NULL,
    attempt_id uuid REFERENCES attempts (id) ON DELETE SET NULL,
    reply text NOT NULL CHECK (reply IN ('rung', 'explained_differently', 'correct_answer', 'essay_feedback')),
    rung smallint CHECK (rung BETWEEN 1 AND 3),
    words text NOT NULL CHECK (char_length(words) BETWEEN 1 AND 4000),
    kind text NOT NULL CHECK (kind IN ('wrong', 'unclear', 'not_for_age', 'harmful', 'personal', 'other')),
    text text NOT NULL CHECK (char_length(text) <= 1000),
    created_at timestamptz NOT NULL,
    reviewed_at timestamptz,
    CHECK ((reply IN ('rung', 'explained_differently')) = (rung IS NOT NULL))
);

CREATE INDEX problem_reports_review ON problem_reports (reviewed_at, created_at);
