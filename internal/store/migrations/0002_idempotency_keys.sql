-- The records of idempotency keys (R17, R35; architecture 3.2 and 3.9). A key lives
-- in its account's space: the same key of another account is another key. A record
-- names the method and the request by its fingerprint, holds the answer once the
-- request is done, and nothing of what the request carried.

-- +goose Up

CREATE TABLE idempotency_keys (
    account_id uuid NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
    key text NOT NULL CHECK (octet_length(key) BETWEEN 1 AND 128),
    -- The procedure of the contract, /novatrail.api.v1.<Service>/<Method>.
    method text NOT NULL,
    -- The profile of a request on one; none on a request of the account itself. The
    -- targets of epic E3 — the attempt, the lesson run — come with their tables.
    profile_id uuid REFERENCES profiles (id) ON DELETE CASCADE,
    -- SHA-256 of the request, or argon2id of one that carries a PIN or an age.
    fingerprint text NOT NULL,
    state text NOT NULL CHECK (state IN ('going', 'done')),
    -- The answer, in the contract's binary protobuf, once the request is done.
    response bytea,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (account_id, key),
    CHECK ((state = 'done') = (response IS NOT NULL))
);

-- The daily job deletes the records older than a day (R17; T28).
CREATE INDEX idempotency_keys_created ON idempotency_keys (created_at);

-- When each wrong PIN of the last quarter of an hour was given, so that no more than
-- five fit in any 15 minutes, across the edge of a window too (R35). It takes the place
-- of pin_failures and pin_failures_since, which nothing reads now; a later migration
-- marked "-- contract:" drops them.
ALTER TABLE accounts ADD COLUMN pin_failed_at timestamptz[] NOT NULL DEFAULT '{}';
