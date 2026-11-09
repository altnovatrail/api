-- The modules connected to a profile (R07, R42; architecture 3.2 and 3.3): the module's
-- ID and when it was connected, with no version. The attempts and lesson runs go on the
-- current version of the pinned index, which the image holds, never on a version kept
-- here. The rows go with their profile.

-- +goose Up

CREATE TABLE connected_modules (
    profile_id uuid NOT NULL REFERENCES profiles (id) ON DELETE CASCADE,
    module_id text NOT NULL CHECK (module_id <> ''),
    connected_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (profile_id, module_id)
);
