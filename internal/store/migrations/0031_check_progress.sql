-- The place of the model that reads the work of a Check still going, and the length of its list,
-- in the record going of its key, for the device to show the Check going on to a later model.
-- contract: the release before this one neither writes nor reads the two columns.

-- +goose Up

ALTER TABLE idempotency_keys
    ADD COLUMN model smallint,
    ADD COLUMN models smallint;
