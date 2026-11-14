-- The stored works of a profile (R113 spec; R70): while the storage of its works is on, the work
-- of each attempt as the device holds it — its history of writing, the context of each submission
-- and its picture —, the last one put of the attempt, which api keeps as bytes and reads no more
-- of. Switching the storage off erases them in its transaction, and they go with the profile.

-- +goose Up

-- A work, by its attempt: what it is, by the attempt's task; its size as the contract counts it,
-- at most 4 MiB; the contract's StoredWork in binary protobuf, with neither its kind nor when it
-- was kept; and when api kept it, the last time it was put.
CREATE TABLE works (
    profile_id uuid NOT NULL REFERENCES profiles (id) ON DELETE CASCADE,
    attempt_id uuid NOT NULL,
    kind text NOT NULL CHECK (kind IN ('handwriting', 'essay', 'drawing')),
    size_bytes integer NOT NULL CHECK (size_bytes BETWEEN 0 AND 4194304),
    work bytea NOT NULL,
    stored_at timestamptz NOT NULL,
    PRIMARY KEY (profile_id, attempt_id),
    FOREIGN KEY (profile_id, attempt_id) REFERENCES attempts (profile_id, id) ON DELETE CASCADE
);

CREATE INDEX works_newest ON works (profile_id, stored_at DESC, attempt_id);
