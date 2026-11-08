-- The account, its invite codes, its consents and its profiles: what epic E2 keeps
-- (architecture 3.1, 3.2 and 3.10; R29). Migrations only go forward: api rolls back
-- only forward (R106 spec), so there is no Down. A migration adds first and removes
-- later: the release still serving while this one rolls out reads the schema as it was,
-- so a statement that drops, renames or narrows comes only in a migration marked
-- "-- contract:" with the reason the old schema is no longer read.

-- +goose Up

-- The account. After its mark "deleting", its row keeps the ID, the sign-in service's
-- UID and the time of the mark, and every other field is cleared, so that the rest is
-- nullable (R21). A request updates fields of this row under FOR KEY SHARE, so none of
-- them is a key: the UID is the one unique field, and it never changes.
CREATE TABLE accounts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    auth_uid text NOT NULL UNIQUE,
    adult_stated_at timestamptz,
    pin_hash text,
    pin_failures integer DEFAULT 0 CHECK (pin_failures >= 0),
    pin_failures_since timestamptz,
    pin_locked_until timestamptz,
    tariff text,
    utc_offset_minutes integer CHECK (utc_offset_minutes BETWEEN -720 AND 840 AND utc_offset_minutes % 15 = 0),
    day_ends_at timestamptz,
    last_request_on date,
    deleting_at timestamptz
);

-- An invite code, by its SHA-256 alone (R04). Redeemed and bound are kept apart: the
-- deletion of an account unbinds its code, which stays redeemed (R21).
CREATE TABLE invite_codes (
    code_sha256 bytea PRIMARY KEY CHECK (octet_length(code_sha256) = 32),
    tariff text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    redeemed_at timestamptz,
    account_id uuid UNIQUE REFERENCES accounts (id) ON DELETE SET NULL,
    CHECK (account_id IS NULL OR redeemed_at IS NOT NULL)
);

-- A learner's profile, with the fields of section 1 and of the contract's Profile. The
-- codes are the contract's. A row about a profile refers to it, so that one written
-- after the profile is gone fails on the key (R21).
CREATE TABLE profiles (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id uuid NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
    owner boolean NOT NULL,
    pseudonym text NOT NULL CHECK (pseudonym <> ''),
    avatar text NOT NULL DEFAULT '',
    age_category text NOT NULL CHECK (age_category IN ('under_13', 'from_13_to_consent', 'from_consent_to_18', 'adult')),
    country text NOT NULL CHECK (country ~ '^[A-Z]{2}$'),
    age_stated_on date,
    voicing_enabled boolean NOT NULL,
    voicing_speed text NOT NULL DEFAULT 'normal' CHECK (voicing_speed IN ('normal', 'slow')),
    default_mode text NOT NULL CHECK (default_mode IN ('by_steps', 'self')),
    interface_language text,
    grade smallint CHECK (grade BETWEEN 1 AND 12),
    ask_before_showing boolean NOT NULL,
    active_on date NOT NULL DEFAULT current_date,
    created_at timestamptz NOT NULL DEFAULT now(),
    -- A profile of the category adult never asks before showing (contract, Profile).
    CHECK (age_category <> 'adult' OR NOT ask_before_showing),
    -- The owner's own profile is an adult's.
    CHECK (NOT owner OR age_category = 'adult')
);

-- One profile of the owner in an account.
CREATE UNIQUE INDEX profiles_one_owner ON profiles (account_id) WHERE owner;
CREATE INDEX profiles_account ON profiles (account_id);

-- A consent: an adult's own, given with the profile, or one for a child, waiting under
-- the account, with its category and country, until it is confirmed (architecture 3.1).
-- What the record writes besides — who gave it and how it was checked — T26 settles.
CREATE TABLE consents (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id uuid NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
    profile_id uuid UNIQUE REFERENCES profiles (id) ON DELETE CASCADE,
    subject text NOT NULL CHECK (subject IN ('adult', 'child')),
    state text NOT NULL CHECK (state IN ('waiting', 'confirmed')),
    age_category text NOT NULL CHECK (age_category IN ('under_13', 'from_13_to_consent', 'from_consent_to_18', 'adult')),
    country text NOT NULL CHECK (country ~ '^[A-Z]{2}$'),
    method text,
    requested_at timestamptz NOT NULL DEFAULT now(),
    confirmed_at timestamptz,
    CHECK ((state = 'confirmed') = (confirmed_at IS NOT NULL)),
    -- A profile only for a confirmed consent; a child's waits without one.
    CHECK (profile_id IS NULL OR state = 'confirmed')
);

CREATE INDEX consents_account ON consents (account_id);

-- What is left of a consent for a child once its profile is deleted: the ID of the
-- record, when it was given, how it was checked and when it was deleted, with no ID of
-- the account or the profile, until the lawyer answers П-14 (R21).
CREATE TABLE child_consent_records (
    id uuid PRIMARY KEY,
    consented_at timestamptz NOT NULL,
    method text NOT NULL,
    deleted_at timestamptz NOT NULL DEFAULT now()
);
