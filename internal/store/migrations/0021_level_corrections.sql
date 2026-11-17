-- The adult's correction of a module's level in the journal of evidence (R89; R157 spec): a row
-- of no attempt, whose evidence is learner-model's correction — the module, the band set, and
-- when —, under an ID of its own, which the export names it by. The package is that of the
-- module's current version when the adult set the level. A row is of an attempt or a
-- correction, never both or neither, and it is only added to, as every row of the journal.
-- contract: the check holds every row there is and every row the release before this one adds,
-- each of an attempt; that release reads the journal joined with the attempts, so a correction
-- is not in its fold, and only its export of a profile with one fails until it is replaced.

-- +goose Up

ALTER TABLE evidence
    ALTER COLUMN attempt_id DROP NOT NULL,
    ADD COLUMN correction_id uuid UNIQUE,
    ADD CHECK (num_nonnulls(attempt_id, correction_id) = 1);
