-- When each check of an own key of the account's last hour was made, so that no more than ten
-- fit in any hour, across the edge of a window too: CheckOwnKey cannot try keys one after
-- another. It holds no key, no provider and no end of a check; the times go with the account.

-- +goose Up

ALTER TABLE accounts ADD COLUMN key_checked_at timestamptz[] NOT NULL DEFAULT '{}';
