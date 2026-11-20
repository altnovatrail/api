-- An attempt of a task from the learner's paper (R10; R162 spec): a row of kind 'paper', of no
-- module, package, task or seed, begun by no choice, lesson run or earlier attempt, beside the
-- attempts of a module, of kind 'module', which hold all of them as before. A profile has one
-- open attempt of a module, and the open attempts of one page beside it. An attempt of paper
-- gives no evidence, and the daily job closes it "abandoned" as any.
-- contract: every row there is, and every row the release before this one adds, is of a module
-- and holds the checks; that release reads an open attempt of the profile by its index, so a
-- profile with a page begun in this release fails its requests on an attempt there until that
-- release is replaced.

-- +goose Up

ALTER TABLE attempts
    ADD COLUMN kind text NOT NULL DEFAULT 'module' CHECK (kind IN ('module', 'paper')),
    ALTER COLUMN package_digest DROP NOT NULL,
    ALTER COLUMN module_id DROP NOT NULL,
    ALTER COLUMN module_version DROP NOT NULL,
    ALTER COLUMN module_format_version DROP NOT NULL,
    ALTER COLUMN module_kit_version DROP NOT NULL,
    ALTER COLUMN task_id DROP NOT NULL,
    ALTER COLUMN seed DROP NOT NULL;

-- The beginning by one of a choice, a lesson run and an earlier attempt holds for a module's
-- attempt alone; the check of migration 0005 has the name Postgres gave it, found by its text.
-- +goose StatementBegin
DO $$
DECLARE
    name text;
BEGIN
    SELECT conname INTO STRICT name FROM pg_constraint
        WHERE conrelid = 'attempts'::regclass AND contype = 'c'
            AND pg_get_constraintdef(oid) LIKE '%num_nonnulls(choice_id, lesson_run_id, previous_attempt_id) = 1%';
    EXECUTE format('ALTER TABLE attempts DROP CONSTRAINT %I', name);
END
$$;
-- +goose StatementEnd

ALTER TABLE attempts
    ADD CONSTRAINT attempts_begun CHECK (kind <> 'module' OR num_nonnulls(choice_id, lesson_run_id, previous_attempt_id) = 1),
    ADD CONSTRAINT attempts_of_kind CHECK (CASE kind
        WHEN 'module' THEN num_nonnulls(package_digest, module_id, module_version, module_format_version, module_kit_version, task_id, seed) = 7
        ELSE num_nonnulls(package_digest, module_id, module_version, module_format_version, module_kit_version, task_id, seed,
            instance, choice_id, lesson_run_id, previous_attempt_id) = 0
    END);

DROP INDEX attempts_one_open;
CREATE UNIQUE INDEX attempts_one_open ON attempts (profile_id) WHERE closed_at IS NULL AND kind = 'module';
-- The open attempts of a page, which a new page closes.
CREATE INDEX attempts_paper_open ON attempts (profile_id) WHERE closed_at IS NULL AND kind = 'paper';
