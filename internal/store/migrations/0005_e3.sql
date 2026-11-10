-- What epic E3 keeps of a learner's work (R43; architecture 3.2): the engine's choices,
-- lesson runs, attempts, the journal of evidence and the cache of the skill state. This is
-- their frame — keys, links, the rules of one open attempt and one open run, and the fields
-- of a row's life; each task of E3 adds the fields its code needs by a migration of its own,
-- adding first. Every row refers to its profile and goes with it (R21), and a row links only
-- to rows of its own profile: each link carries the profile. The codes are the contract's.

-- +goose Up

-- An engine's choice (R06): of a task or of a lesson. What it chose and why, the tasks of
-- E3 add.
CREATE TABLE engine_choices (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    profile_id uuid NOT NULL REFERENCES profiles (id) ON DELETE CASCADE,
    kind text NOT NULL CHECK (kind IN ('task', 'lesson')),
    chosen_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (profile_id, id, kind)
);

CREATE INDEX engine_choices_profile ON engine_choices (profile_id, chosen_at);

-- A lesson run (R13): one learner's lesson on the package it began on. A choice of a lesson
-- opens one run, and a profile has one open run of a lesson, while runs of other lessons may
-- be open. Its beginning, and its end once written, do not change.
CREATE TABLE lesson_runs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    profile_id uuid NOT NULL REFERENCES profiles (id) ON DELETE CASCADE,
    -- The lesson's full ID: the module's ID and the lesson's local ID.
    lesson_id text NOT NULL CHECK (char_length(lesson_id) BETWEEN 1 AND 256),
    module_id text NOT NULL CHECK (char_length(module_id) BETWEEN 1 AND 128),
    module_version text NOT NULL CHECK (char_length(module_version) BETWEEN 1 AND 64),
    package_digest text NOT NULL CHECK (package_digest ~ '^sha256:[0-9a-f]{64}$'),
    choice_id uuid NOT NULL UNIQUE,
    -- The kind of the choice it is opened by, which the link holds to a lesson's.
    choice_kind text NOT NULL GENERATED ALWAYS AS ('lesson') STORED,
    started_at timestamptz NOT NULL DEFAULT now(),
    last_action_at timestamptz NOT NULL DEFAULT now(),
    end_reason text CHECK (end_reason IN ('passed', 'unchecked', 'not_suitable', 'abandoned')),
    ended_at timestamptz,
    CHECK ((end_reason IS NULL) = (ended_at IS NULL)),
    UNIQUE (profile_id, id),
    FOREIGN KEY (profile_id, choice_id, choice_kind) REFERENCES engine_choices (profile_id, id, kind)
);

CREATE UNIQUE INDEX lesson_runs_one_open ON lesson_runs (profile_id, lesson_id) WHERE ended_at IS NULL;
-- What the daily job ends "abandoned".
CREATE INDEX lesson_runs_open_since ON lesson_runs (last_action_at) WHERE ended_at IS NULL;

-- An attempt (R07, R10): of a task, on the package, the format version and the module-kit
-- by which its verdict repeats, with its seed. It begins by an engine's choice of a task, on
-- a task segment of a lesson run, or again after an earlier attempt of its task — one of
-- the three. A profile has one open attempt. Its beginning, and its close once written, do
-- not change.
CREATE TABLE attempts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    profile_id uuid NOT NULL REFERENCES profiles (id) ON DELETE CASCADE,
    package_digest text NOT NULL CHECK (package_digest ~ '^sha256:[0-9a-f]{64}$'),
    module_id text NOT NULL CHECK (char_length(module_id) BETWEEN 1 AND 128),
    module_version text NOT NULL CHECK (char_length(module_version) BETWEEN 1 AND 64),
    module_format_version text NOT NULL CHECK (char_length(module_format_version) BETWEEN 1 AND 16),
    module_kit_version text NOT NULL CHECK (char_length(module_kit_version) BETWEEN 1 AND 64),
    task_id text NOT NULL CHECK (char_length(task_id) BETWEEN 1 AND 128),
    -- The seed, module-kit's uint64, as the bits of a bigint; it never reaches the device.
    seed bigint NOT NULL,
    -- The instance the seed chose, when the task is a template.
    instance integer CHECK (instance >= 0),
    choice_id uuid UNIQUE,
    -- The kind of the choice it begins by, which the link holds to a task's.
    choice_kind text NOT NULL GENERATED ALWAYS AS ('task') STORED,
    lesson_run_id uuid,
    -- The segment's place in the lesson, from 1.
    segment integer CHECK (segment >= 1),
    previous_attempt_id uuid,
    started_at timestamptz NOT NULL DEFAULT now(),
    last_action_at timestamptz NOT NULL DEFAULT now(),
    close_reason text CHECK (close_reason IN ('correct', 'solution_shown', 'left', 'abandoned', 'unchecked', 'not_suitable', 'done')),
    closed_at timestamptz,
    CHECK ((close_reason IS NULL) = (closed_at IS NULL)),
    CHECK ((lesson_run_id IS NULL) = (segment IS NULL)),
    CHECK (num_nonnulls(choice_id, lesson_run_id, previous_attempt_id) = 1),
    UNIQUE (profile_id, id),
    FOREIGN KEY (profile_id, choice_id, choice_kind) REFERENCES engine_choices (profile_id, id, kind),
    FOREIGN KEY (profile_id, lesson_run_id) REFERENCES lesson_runs (profile_id, id),
    FOREIGN KEY (profile_id, previous_attempt_id) REFERENCES attempts (profile_id, id)
);

CREATE UNIQUE INDEX attempts_one_open ON attempts (profile_id) WHERE closed_at IS NULL;
-- What the daily job closes "abandoned".
CREATE INDEX attempts_open_since ON attempts (last_action_at) WHERE closed_at IS NULL;
CREATE INDEX attempts_profile ON attempts (profile_id, started_at);
-- The links a deletion follows.
CREATE INDEX attempts_lesson_run ON attempts (profile_id, lesson_run_id) WHERE lesson_run_id IS NOT NULL;
CREATE INDEX attempts_previous ON attempts (profile_id, previous_attempt_id) WHERE previous_attempt_id IS NOT NULL;

-- The beginning of a row, and its end once written, do not change (R10, R13).
-- +goose StatementBegin
CREATE FUNCTION attempt_is_kept() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.started_at IS DISTINCT FROM OLD.started_at
        OR (OLD.closed_at IS NOT NULL
            AND (NEW.close_reason IS DISTINCT FROM OLD.close_reason OR NEW.closed_at IS DISTINCT FROM OLD.closed_at)) THEN
        RAISE EXCEPTION 'the beginning and the close of an attempt do not change (R10)' USING ERRCODE = 'restrict_violation';
    END IF;
    RETURN NEW;
END
$$;
-- +goose StatementEnd

CREATE TRIGGER attempts_kept BEFORE UPDATE ON attempts FOR EACH ROW EXECUTE FUNCTION attempt_is_kept();

-- +goose StatementBegin
CREATE FUNCTION lesson_run_is_kept() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.started_at IS DISTINCT FROM OLD.started_at
        OR (OLD.ended_at IS NOT NULL
            AND (NEW.end_reason IS DISTINCT FROM OLD.end_reason OR NEW.ended_at IS DISTINCT FROM OLD.ended_at)) THEN
        RAISE EXCEPTION 'the beginning and the end of a lesson run do not change (R13)' USING ERRCODE = 'restrict_violation';
    END IF;
    RETURN NEW;
END
$$;
-- +goose StatementEnd

CREATE TRIGGER lesson_runs_kept BEFORE UPDATE ON lesson_runs FOR EACH ROW EXECUTE FUNCTION lesson_run_is_kept();

-- The journal of evidence (R06; R80 spec): one piece of evidence of a closed attempt, as
-- learner-model's type holds it, under its number in the profile's journal. The journal is
-- only added to: no row changes, and rows go only with their profile.
CREATE TABLE evidence (
    profile_id uuid NOT NULL REFERENCES profiles (id) ON DELETE CASCADE,
    seq bigint NOT NULL CHECK (seq >= 1),
    attempt_id uuid NOT NULL UNIQUE,
    package_digest text NOT NULL CHECK (package_digest ~ '^sha256:[0-9a-f]{64}$'),
    module_id text NOT NULL CHECK (char_length(module_id) BETWEEN 1 AND 128),
    recorded_at timestamptz NOT NULL DEFAULT now(),
    evidence bytea NOT NULL,
    PRIMARY KEY (profile_id, seq),
    FOREIGN KEY (profile_id, attempt_id) REFERENCES attempts (profile_id, id)
);

-- A change of the journal, a row deleted by itself rather than by the cascade of its
-- profile's deletion, which runs at a deeper level of triggers, and a truncation are
-- refused.
-- +goose StatementBegin
CREATE FUNCTION evidence_is_appended_only() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' AND pg_trigger_depth() > 1 THEN
        RETURN OLD;
    END IF;
    RAISE EXCEPTION 'the journal of evidence is only added to (R06)' USING ERRCODE = 'restrict_violation';
END
$$;
-- +goose StatementEnd

CREATE TRIGGER evidence_appended_only BEFORE UPDATE OR DELETE ON evidence
    FOR EACH ROW EXECUTE FUNCTION evidence_is_appended_only();
CREATE TRIGGER evidence_not_truncated BEFORE TRUNCATE ON evidence
    FOR EACH STATEMENT EXECUTE FUNCTION evidence_is_appended_only();

-- The cache of the skill state (R06): a row for each profile, made with it, empty and at
-- number 0, so that the first evidence has a row to lock. Its mark — the version of
-- learner-model, the release of modules and the window of the format — says when it is
-- stale; the state is learner-model's type as is.
CREATE TABLE skill_states (
    profile_id uuid PRIMARY KEY REFERENCES profiles (id) ON DELETE CASCADE,
    state bytea,
    learner_model_version text,
    modules_release text,
    format_window text,
    last_seq bigint NOT NULL DEFAULT 0 CHECK (last_seq >= 0)
);

INSERT INTO skill_states (profile_id) SELECT id FROM profiles;

-- The targets of the idempotency keys of E3 (migration 0002): the attempt and the run, of
-- the key's own profile.
ALTER TABLE idempotency_keys
    ADD COLUMN attempt_id uuid,
    ADD COLUMN lesson_run_id uuid,
    ADD FOREIGN KEY (profile_id, attempt_id) REFERENCES attempts (profile_id, id) ON DELETE CASCADE,
    ADD FOREIGN KEY (profile_id, lesson_run_id) REFERENCES lesson_runs (profile_id, id) ON DELETE CASCADE;

CREATE INDEX idempotency_keys_attempt ON idempotency_keys (profile_id, attempt_id) WHERE attempt_id IS NOT NULL;
CREATE INDEX idempotency_keys_lesson_run ON idempotency_keys (profile_id, lesson_run_id) WHERE lesson_run_id IS NOT NULL;
