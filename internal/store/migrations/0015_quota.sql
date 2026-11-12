-- The quota of platform AI (R11, R12, R57; architecture 3.6): a counter of each profile and of
-- each account for each family day that had a reserve — spent and in reserve, in whole
-- micro-dollars of tutor's estimate —, and the one row of the flag "platform AI closed" of the
-- deployment. A counter is keyed by the beginning of its day, not by a date: a change of the offset
-- may give a date twice. It keeps the end of its day too, by which the daily job deletes it a day
-- after (R12). The deletion of a profile takes its counters by their key; the account's stay until
-- the account goes (R12, R21).

-- +goose Up

CREATE TABLE account_quota (
    account_id uuid NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
    day_start timestamptz NOT NULL,
    day_end timestamptz NOT NULL,
    spent_micro_usd bigint NOT NULL DEFAULT 0 CHECK (spent_micro_usd >= 0),
    reserved_micro_usd bigint NOT NULL DEFAULT 0 CHECK (reserved_micro_usd >= 0),
    PRIMARY KEY (account_id, day_start),
    CHECK (day_start < day_end)
);

CREATE INDEX account_quota_day_end ON account_quota (day_end);

CREATE TABLE profile_quota (
    profile_id uuid NOT NULL REFERENCES profiles (id) ON DELETE CASCADE,
    day_start timestamptz NOT NULL,
    day_end timestamptz NOT NULL,
    spent_micro_usd bigint NOT NULL DEFAULT 0 CHECK (spent_micro_usd >= 0),
    reserved_micro_usd bigint NOT NULL DEFAULT 0 CHECK (reserved_micro_usd >= 0),
    PRIMARY KEY (profile_id, day_start),
    CHECK (day_start < day_end)
);

CREATE INDEX profile_quota_day_end ON profile_quota (day_end);

-- The flag "platform AI closed": one row, closed since closed_at, open when it is NULL. A refusal of
-- the provider by its limit on the platform's key closes it, and only a command of the operator
-- opens it again (R11).
CREATE TABLE platform_ai (
    one boolean PRIMARY KEY DEFAULT true CHECK (one),
    closed_at timestamptz
);

INSERT INTO platform_ai DEFAULT VALUES;
