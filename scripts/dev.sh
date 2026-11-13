#!/usr/bin/env bash
# The run of api on the dev projects of Firebase and Neon (R39): `just dev <command>` runs a command
# of the binary with the configuration of the dev file on the stub of tutor, which calls no provider
# (R66), `just dev-ai <command>` the same with the dev file's key of platform AI, on the models, and
# `just dev-check` goes the whole way —
# migrations, an invite code, a country, the packages of modules, the opening of platform AI, the
# server, an ID token, the legal texts, the account, the owner's profile, its progress and "something
# else", a reading of handwriting with its reserve and settlement of the quota, a parent's consent by
# its form, then the job after a rollout — and checks the answers and the logs. The server's key of
# platform AI is one Anthropic refuses, so that no call of the check reaches a model. `just dev-app`
# keeps the server up on the stub for the local build of app, which signs in by a custom token this
# script lays in a volume the two devcontainers share (R67).
#
# The configuration is KEY=VALUE lines in a file outside any git working tree, by default in the
# volume the devcontainer mounts (R39): its lines are read as they are, never run, only names of
# api's own, API_*, are taken, and no value of it is printed. What the binary does not read — the
# Web API key, the UID of the dev account — has names of the recipe's own, API_DEV_*, which the
# binary ignores. No secret goes on a command line, where any process could read it: they go
# through the environment, files and pipes.
#
# scripts/dev_test.go tests the loader, the custom token and how it is laid for app, the task AI reads
# and the counters in CI.
set -euo pipefail
shopt -s inherit_errexit

config=${API_DEV_CONFIG:-/home/vscode/.novatrail-dev/dev.env}
# The account of the dev project is a test one, by a UID of its own (R39).
uid_default=dev-author
# The account the local build of app signs in as, a test one of its own, apart from the check's, so
# that the runs of app spend neither its quota nor its data (R67).
app_uid_default=dev-app
# The folder of the volume the devcontainers of api and app share, where app finds the custom token
# of its account (R67). A custom token lasts an hour at most, so it is laid anew every half hour, and
# the one in the folder always has half an hour left.
token_dir_default=/home/vscode/.novatrail-token
token_every=1800
# What the steps the check and the run for app share print first.
who=dev
# Synthetic, as the account is.
country=US
# The binary the recipes run, built as `just build` builds it; bin/ is ignored by git.
binary=bin/api
# The key of platform AI the check's server has instead of the dev file's: one Anthropic refuses,
# so that a reading of handwriting goes to the provider and back, free, before any model reads it,
# and no call of the check costs anything (R39).
refused_key=sk-ant-dev-check-refused
# The work the check sends to be read: a synthetic PNG of one white pixel.
pixel=iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAAAAAA6fptVAAAAD0lEQVR4nAACAP3/Av8DAAEFAQJnLD7dAAAAAElFTkSuQmCC

die() {
    echo "dev: $*" >&2
    exit 1
}

# in_working_tree says whether the directory $1 or one of its parents holds .git: the binary's own
# rule for a secret's file (internal/config/secret.go), which needs no git and cannot fail open.
in_working_tree() {
    local dir=$1
    while :; do
        [[ -e $dir/.git ]] && return 0
        [[ $dir == / ]] && return 1
        dir=$(dirname "$dir")
    done
}

# load exports the API_* variables of the dev file. A line that is not KEY=VALUE, or names another
# variable, is named by its number, not its text, which may hold a secret.
load() {
    [[ -f $config ]] || die "no configuration at $config: put dev.env there, as README.md shows, or name another file by API_DEV_CONFIG"
    if in_working_tree "$(dirname "$(realpath "$config")")"; then
        die "$config is in a git working tree: keep it outside any repository"
    fi
    local line n=0
    while IFS= read -r line || [[ -n $line ]]; do
        n=$((n + 1))
        line=${line%$'\r'}
        [[ $line =~ ^[[:space:]]*(#.*)?$ ]] && continue
        [[ $line =~ ^(API_[A-Z0-9_]+)=(.*)$ ]] || die "$config, line $n, is not API_NAME=VALUE"
        export "${BASH_REMATCH[1]}=${BASH_REMATCH[2]}"
    done < "$config"
}

# stubbed puts the run on the stub of tutor (R66): no key of platform AI from the dev file reaches
# the process, and every call of AI is the stub's, which costs nothing. `just dev` runs so.
stubbed() {
    unset API_ANTHROPIC_KEY API_ANTHROPIC_KEY_FILE
    export API_TUTOR_STUB=1
}

# modelled puts the run on the models, with the dev file's key: `just dev-ai`, for a task that checks
# the words of a model under the cap its text names (rule 4 of RUN.md; R146 spec).
modelled() {
    unset API_TUTOR_STUB
}

# b64url is base64url with no padding, as a JWT writes its parts.
b64url() { openssl base64 -A | tr '+/' '-_' | tr -d '='; }

# signing_key prints the path of the service account's key the dev file names.
signing_key() {
    local key=${API_FIREBASE_CREDENTIALS_FILE:-}
    [[ -n $key && -f $key ]] || die "API_FIREBASE_CREDENTIALS_FILE must name the service account's key"
    printf '%s\n' "$key"
}

# custom_token prints a custom token of the account $2 of the dev project, signed by RS256 with the
# service account's key in the file $1: the claims are Firebase's for a custom token made with a JWT
# library (R39). The key reaches openssl through a pipe.
custom_token() {
    local email now header payload unsigned signature
    email=$(jq -er .client_email "$1") || die "the key has no client_email"
    now=$(date +%s)
    header=$(printf '{"alg":"RS256","typ":"JWT"}' | b64url)
    payload=$(EMAIL=$email UID_=$2 NOW=$now jq -cn '{
        iss: env.EMAIL, sub: env.EMAIL,
        aud: "https://identitytoolkit.googleapis.com/google.identity.identitytoolkit.v1.IdentityToolkit",
        iat: (env.NOW | tonumber), exp: ((env.NOW | tonumber) + 3600), uid: env.UID_}' | b64url)
    unsigned="$header.$payload"
    signature=$(printf '%s' "$unsigned" | openssl dgst -sha256 -sign <(jq -er .private_key "$1") | b64url) ||
        die "the key would not sign the custom token"
    printf '%s.%s\n' "$unsigned" "$signature"
}

# exchange prints the ID token the sign-in service gives for the custom token on stdin (R39). The
# Web API key reaches curl through its configuration on stdin, the custom token through the
# environment and a pipe.
exchange() {
    local custom
    [[ -n ${API_DEV_WEB_API_KEY:-} ]] || die "API_DEV_WEB_API_KEY must be set: the Web API key of the dev project"
    custom=$(cat)
    printf 'url = "https://identitytoolkit.googleapis.com/v1/accounts:signInWithCustomToken?key=%s"\n' "$API_DEV_WEB_API_KEY" |
        curl -sS --fail -K - -H 'Content-Type: application/json' \
            -d @<(TOKEN=$custom jq -cn '{token: env.TOKEN, returnSecureToken: true}') |
        jq -er .idToken || die "the sign-in service did not exchange the custom token"
}

# id_token prints an ID token of the account $1 of the dev project: its custom token exchanged at the
# sign-in service (R39).
id_token() {
    local key custom
    key=$(signing_key)
    custom=$(custom_token "$key" "$1")
    printf '%s' "$custom" | exchange
}

# token_folder fails unless the folder $1 for the token is there. It is the volume the devcontainers
# of api and app share: one that is not there is not made here, since app would not see it.
token_folder() {
    [[ -d $1 ]] || die "no folder $1 for the token: the devcontainer mounts the volume novatrail-dev-token there after Rebuild Container, or name another folder by API_DEV_TOKEN_DIR"
}

# lay_token lays a custom token of the account $2 in the folder $1 as custom-token, readable by its
# owner alone, by a rename, so that app never reads half a token (R67).
lay_token() {
    local dir=$1 key token next
    token_folder "$dir"
    key=$(signing_key)
    token=$(custom_token "$key" "$2")
    next=$(mktemp "$dir/.custom-token.XXXXXX")
    printf '%s\n' "$token" > "$next"
    mv -f -- "$next" "$dir/custom-token"
}

# call posts the body on stdin to the method $1 as JSON with the token — of AccountService, or
# Service/Method of another service; the answer goes to $work/answer.json and its HTTP status to
# $status.
call() {
    local method=$1
    [[ $method == */* ]] || method=AccountService/$method
    status=$(curl -sS -o "$work/answer.json" -w '%{http_code}' \
        -H 'Content-Type: application/json' -H @<(printf 'Authorization: Bearer %s\n' "$token") \
        -d @- "$base/novatrail.api.v1.$method") || die "$1: api did not answer"
}

# expect fails the check, naming the step, unless the last call answered 200.
expect() {
    [[ $status == 200 ]] || die "$1 answered $status: $(jq -c '{code, message}' "$work/answer.json" 2> /dev/null || true)"
}

# secret prints the value of the secret $1: its variable, or the file its twin _FILE names.
secret() {
    local file_var="${1}_FILE"
    if [[ -n ${!1:-} ]]; then
        printf '%s' "${!1}"
    elif [[ -n ${!file_var:-} && -f ${!file_var} ]]; then
        cat -- "${!file_var}"
    fi
}

# clean fails the check unless the log $1 is JSON, line by line, and holds none of what the logs
# must not: the UID — $2, or the check's —, an email, a connection string, the Web API key, the
# token (R09 spec).
clean() {
    jq -e . "$1" > /dev/null || die "a line of $(basename "$1") is not JSON"
    ! grep -qF -- "${2:-${API_DEV_UID:-$uid_default}}" "$1" || die "$(basename "$1") names the UID"
    ! grep -qE '[[:alnum:]._%+-]+@[[:alnum:].-]+\.[[:alpha:]]{2,}' "$1" || die "$(basename "$1") holds an email"
    local name value
    for name in API_DATABASE_URL API_OPENFGA_DATABASE_URL API_MIGRATE_DATABASE_URL API_MIGRATE_OPENFGA_DATABASE_URL \
        API_DEV_WEB_API_KEY API_ANTHROPIC_KEY; do
        value=$(secret "$name")
        [[ -z $value ]] || ! grep -qF -- "$value" "$1" || die "$(basename "$1") holds $name"
    done
    ! grep -qF -- "$refused_key" "$1" || die "$(basename "$1") holds the key of platform AI"
    [[ -z ${token:-} ]] || ! grep -qF -- "$token" "$1" || die "$(basename "$1") holds the token"
}

# handwritten says whether the attempt $1, an AttemptStart as JSON, is on a task that AI reads: one
# with an answer slot written anywhere on the sheet.
handwritten() {
    jq -e '.task.json | @base64d | fromjson | any(.. | objects; .binding? == "anywhere")' "$1" > /dev/null
}

# counted prints what the export's file $1 holds of the quota of platform AI, over all its family
# days: the micro-dollars spent and those in reserve, "0 0" when it holds no counter (R57).
counted() {
    jq -er '.quota_counters // [] | "\(map(.spent_micro_usd) | add // 0) \(map(.reserved_micro_usd) | add // 0)"' "$1"
}

# exported unpacks the account's export into the folder $1 (R52).
exported() {
    call ExportAccount <<< '{}'
    expect ExportAccount
    mkdir -p "$1"
    jq -r .part "$work/answer.json" | base64 -d > "$1.zip" || die "ExportAccount gave a part that is not base64"
    unzip -q -o "$1.zip" -d "$1" || die "ExportAccount gave a part that is not a zip"
}

# handwriting begins an attempt of the profile $1 on a task that AI reads and leaves its
# AttemptStart in $work/attempt.json: the engine's task, or the next segment of the lesson it gives,
# asking it again while what begins is checked by tap or is not admitted.
handwriting() {
    local profile=$1 try kind choice run segment
    for try in $(seq 8); do
        call LearningService/ChooseNext < <(PROFILE=$profile KEY=$(cat /proc/sys/kernel/random/uuid) \
            jq -cn '{profile_id: env.PROFILE, idempotency_key: env.KEY}')
        expect ChooseNext
        kind=$(jq -r .choice.kind "$work/answer.json")
        choice=$(jq -r .choice.choice_id "$work/answer.json")
        case $kind in
        CHOICE_KIND_TASK)
            call LearningService/BeginAttempt < <(PROFILE=$profile CHOICE=$choice KEY=$(cat /proc/sys/kernel/random/uuid) \
                jq -cn '{profile_id: env.PROFILE, idempotency_key: env.KEY, choice_id: env.CHOICE}')
            ;;
        CHOICE_KIND_LESSON)
            call LearningService/OpenLesson < <(PROFILE=$profile CHOICE=$choice jq -cn '{profile_id: env.PROFILE, choice_id: env.CHOICE}')
            expect OpenLesson
            run=$(jq -r .run.lesson_run_id "$work/answer.json")
            # Segments begin in their order: the first task segment the run has not ended (R48).
            segment=$(jq -r '(.run.ended_segments // []) as $ended | .run.lesson.json | @base64d | fromjson |
                [.segments | to_entries[] | select(.value.task != null) | .key + 1 | select(. as $p | $ended | index($p) | not)] |
                first // empty' "$work/answer.json")
            [[ -n $segment ]] || continue
            call LearningService/BeginAttempt < <(PROFILE=$profile RUN=$run SEGMENT=$segment KEY=$(cat /proc/sys/kernel/random/uuid) \
                jq -cn '{profile_id: env.PROFILE, idempotency_key: env.KEY, segment: {lesson_run_id: env.RUN, segment: (env.SEGMENT | tonumber)}}')
            ;;
        *)
            die "ChooseNext gave $(jq -c .choice "$work/answer.json"), want a lesson or a task"
            ;;
        esac
        [[ $status == 200 ]] || continue
        jq .attempt "$work/answer.json" > "$work/attempt.json"
        handwritten "$work/attempt.json" && return 0
    done
    die "the engine gave no task that AI reads in 8 choices; the last answer: $(jq -c '{code, message}' "$work/answer.json" 2> /dev/null || true)"
}

stop_serve() {
    if [[ -n ${serve_pid:-} ]] && kill -0 "$serve_pid" 2> /dev/null; then
        kill -INT "$serve_pid"
        wait "$serve_pid" || true
    fi
}

build() {
    CGO_ENABLED=0 go build -trimpath -o "$binary" ./cmd/api
}

# migrated applies the migrations of both databases and allows the country of the dev accounts.
migrated() {
    echo "$who: migrate"
    "$binary" migrate > "$work/migrate.log" || die "migrate failed: $(cat "$work/migrate.log")"
    clean "$work/migrate.log"
    echo "$who: allow $country"
    "$binary" countries allow "$country" > /dev/null
}

# packaged lays the packages of the pinned release into $work/packages, afresh for this run, as the
# image's build lays them (R41): a folder of the dev file stays as the author laid it, and the server
# of the run starts on these whatever release that folder holds.
packaged() {
    echo "$who: packages"
    "$binary" packages "$work/packages" > "$work/packages.log" || die "packages failed: $(cat "$work/packages.log")"
    clean "$work/packages.log"
}

# reopened runs the operator's command that opens platform AI again (R59), before the server: a flag
# the provider's limit left set would answer a reading "platform limit" with no call.
reopened() {
    echo "$who: reopen"
    "$binary" reopen > "$work/reopen.out" 2> "$work/reopen.log" || die "reopen failed: $(cat "$work/reopen.log")"
    clean "$work/reopen.log"
    echo "$who: $(cat "$work/reopen.out")"
}

# started waits for the line startup in the log $1 of the server serve_pid and prints its port.
started() {
    local port=""
    for _ in $(seq 60); do
        port=$(jq -r 'select(.message == "startup") | .port' "$1" 2> /dev/null || true)
        [[ -n $port ]] && break
        kill -0 "$serve_pid" 2> /dev/null || die "serve did not start: $(cat "$1")"
        sleep 0.5
    done
    [[ -n $port ]] || die "serve did not start in 30 s"
    printf '%s\n' "$port"
}

# legal_texts asks for the legal texts, which go with no token, and keeps them in $work/texts.json: a
# new account names the current versions of the policy and the terms (R52; R130, R136 spec).
legal_texts() {
    call LegalService/GetLegalTexts <<< '{}'
    expect GetLegalTexts
    jq -e '[.texts[]?.id] | index("privacy-policy") != null and index("terms-of-use") != null' "$work/answer.json" > /dev/null ||
        die "GetLegalTexts gave $(jq -c '[.texts[]?.id]' "$work/answer.json"), want the policy and the terms"
    cp "$work/answer.json" "$work/texts.json"
    echo "$who: the legal texts are there"
}

# owner_account makes sure the account of the token is there with an owner's profile of the category
# adult: an invite code and CreateAccount when there is no account, CreateOwnerProfile when there is
# no owner's profile, so that a repeat makes nothing. GetAccount's answer is left in
# $work/answer.json.
owner_account() {
    call GetAccount <<< '{}'
    # A UID with no account is refused as permission_denied, CAUSE_NO_ACCESS (R04).
    if [[ $status == 403 ]] && [[ $(jq -r .code "$work/answer.json") == permission_denied ]]; then
        echo "$who: no account yet: an invite code and CreateAccount"
        local code
        code=$("$binary" invite 2> "$work/invite.log") || die "invite failed: $(cat "$work/invite.log")"
        call CreateAccount < <(CODE=$code jq -c '{invite_code: env.CODE, adult_stated: true,
            accepted_texts: [.texts[] | select(.id == "privacy-policy" or .id == "terms-of-use") | {id, version}]}' "$work/texts.json")
        expect CreateAccount
        call GetAccount <<< '{}'
    fi
    expect GetAccount
    local owners
    owners=$(jq '[.account.profiles[]? | select(.owner)] | length' "$work/answer.json")
    if [[ $owners == 0 ]]; then
        echo "$who: no owner's profile yet: CreateOwnerProfile"
        call CreateOwnerProfile < <(COUNTRY=$country jq -cn '{country: env.COUNTRY, settings: {pseudonym: "Dev"}}')
        expect CreateOwnerProfile
        call GetAccount <<< '{}'
        expect GetAccount
    fi
    jq -e '(.account.account_id // "") != "" and
        ([.account.profiles[]? | select(.owner and .age_category == "adult")] | length) == 1' \
        "$work/answer.json" > /dev/null || die "GetAccount does not hold the account with the owner's profile, adult"
    echo "$who: the account and the owner's profile are there"
}

check() {
    # The check calls the provider with the key it refuses, not the stub (R39).
    who=dev-check
    modelled
    work=$(mktemp -d)
    trap 'stop_serve; rm -rf "$work"' EXIT
    echo "$who: build"
    build

    # What the dev file declares for admission, checked before the migrations as the rollout of
    # infra checks a deployment's (R45).
    echo "$who: check"
    "$binary" check > "$work/check.log" 2>&1 || die "check failed: $(cat "$work/check.log")"
    clean "$work/check.log"

    migrated
    packaged
    reopened

    # The server has the key Anthropic refuses, not the dev file's: an empty _FILE is one not set.
    echo "$who: serve"
    mkdir -p "$work/forms"
    API_ANTHROPIC_KEY_FILE='' API_ANTHROPIC_KEY=$refused_key \
        API_PACKAGES_DIR="$work/packages" API_CONSENT_FORMS_DIR="$work/forms" PORT=0 "$binary" serve > "$work/serve.log" 2>&1 &
    serve_pid=$!
    local port
    port=$(started "$work/serve.log")
    base="http://127.0.0.1:$port"

    echo "$who: ID token"
    token=$(id_token "${API_DEV_UID:-$uid_default}")

    legal_texts
    owner_account

    # The progress of the owner's profile settles its evidence first, under the lock of its row of
    # the cache, on the database itself (R49).
    local owner
    owner=$(jq -r '.account.profiles[] | select(.owner) | .profile_id' "$work/answer.json")
    call ProgressService/GetProgress < <(PROFILE=$owner jq -cn '{profile_id: env.PROFILE}')
    expect GetProgress
    echo "$who: the progress of the owner's profile is there"

    # The module of the first app on the pinned release (R53): in the owner's catalog, connected —
    # a repeat answers by the profile's state —, a file of its package's part for the device, and
    # "something else", which asks the engine on the database itself (R50), gives a lesson or a task.
    call ModuleService/ListCatalog < <(PROFILE=$owner jq -cn '{profile_id: env.PROFILE}')
    expect ListCatalog
    local digest
    digest=$(jq -r '.modules[]? | select(.module_id == "novatrail/olympiad-math") | .package_digest' "$work/answer.json")
    [[ -n $digest ]] || die "ListCatalog gave $(jq -c '[.modules[]?.module_id]' "$work/answer.json"), want novatrail/olympiad-math"
    call ModuleService/ConnectModule < <(PROFILE=$owner jq -cn '{profile_id: env.PROFILE, module_id: "novatrail/olympiad-math"}')
    expect ConnectModule
    local file
    file=$(curl -sS -o /dev/null -w '%{http_code}' "$base/packages/${digest/:/-}/device/LICENSE") || die "the package's LICENSE: api did not answer"
    [[ $file == 200 ]] || die "the package's LICENSE answered $file, want 200"
    call LearningService/ChooseNext < <(PROFILE=$owner KEY=$(cat /proc/sys/kernel/random/uuid) \
        jq -cn '{profile_id: env.PROFILE, idempotency_key: env.KEY}')
    expect ChooseNext
    jq -e '.choice.kind == "CHOICE_KIND_LESSON" or .choice.kind == "CHOICE_KIND_TASK"' "$work/answer.json" > /dev/null ||
        die "ChooseNext gave $(jq -c .choice "$work/answer.json"), want a lesson or a task"
    echo "$who: olympiad-math is connected, and the engine gives a $(jq -r '.choice.kind | ltrimstr("CHOICE_KIND_") | ascii_downcase' "$work/answer.json")"

    # The quota of platform AI on the database itself (R58): a reading of handwriting takes its
    # reserve before the call and settles it after. The provider refuses the key, the answer has no
    # record of usage, and the reserve is kept as spent (R12): the export shows more spent and the
    # same in reserve.
    local before after attempt spent_before reserved_before spent_after reserved_after
    exported "$work/before"
    before=$(counted "$work/before/profiles/$owner.json")
    handwriting "$owner"
    attempt=$(jq -r .attempt_id "$work/attempt.json")
    call AttemptService/SubmitWork < <(PROFILE=$owner ATTEMPT=$attempt KEY=$(cat /proc/sys/kernel/random/uuid) PIXEL=$pixel \
        jq -cn '{profile_id: env.PROFILE, attempt_id: env.ATTEMPT, idempotency_key: env.KEY, work: {sheet: {crop: env.PIXEL}},
            clock_offset_minutes: 0}')
    expect SubmitWork
    jq -e '.outcome.cause == "CAUSE_KEY_REFUSED"' "$work/answer.json" > /dev/null ||
        die "SubmitWork gave the outcome $(jq -c .outcome "$work/answer.json"), want CAUSE_KEY_REFUSED"
    exported "$work/after"
    after=$(counted "$work/after/profiles/$owner.json")
    read -r spent_before reserved_before <<< "$before"
    read -r spent_after reserved_after <<< "$after"
    ((spent_after > spent_before && reserved_after == reserved_before)) ||
        die "the profile's counters went from spent and reserved $before to $after, want more spent and the same reserved"
    before=$(counted "$work/before/account.json")
    after=$(counted "$work/after/account.json")
    read -r spent_before reserved_before <<< "$before"
    read -r spent_after reserved_after <<< "$after"
    ((spent_after > spent_before && reserved_after == reserved_before)) ||
        die "the account's counters went from spent and reserved $before to $after, want more spent and the same reserved"
    echo "$who: a reading of handwriting takes its reserve and settles it, on both counters"

    # A parent's consent for a synthetic child of 9: its request by a code, the scan of its form in
    # the folder of the forms, the form among those the operator checks, then the consent taken back,
    # so that nothing of it is left (R52; R116 spec).
    call RequestChildConsent < <(KEY=$(cat /proc/sys/kernel/random/uuid) jq -c '{idempotency_key: env.KEY, age_years: 9,
        country: "US", parent_consent_text: (.texts[] | select(.id == "parent-consent") | {id, version})}' "$work/texts.json")
    expect RequestChildConsent
    local consent request
    consent=$(jq -r .consent.consent_id "$work/answer.json")
    request=$(jq -r .consent.request_code "$work/answer.json")
    call SendConsentForm < <(CONSENT=$consent KEY=$(cat /proc/sys/kernel/random/uuid) jq -cn '{consent_id: env.CONSENT,
        idempotency_key: env.KEY, form: ("synthetic scan" | @base64), media_type: "image/png"}')
    expect SendConsentForm
    local scans=("$work/forms/$consent"-*)
    [[ -f ${scans[0]} ]] || die "SendConsentForm laid no scan in the folder of the forms"
    "$binary" consents list > "$work/consents.out" 2> "$work/consents.log" || die "consents list failed: $(cat "$work/consents.log")"
    grep -q "^$request" "$work/consents.out" || die "consents list does not name the form sent"
    call CancelChildConsent < <(CONSENT=$consent jq -cn '{consent_id: env.CONSENT}')
    expect CancelChildConsent
    echo "$who: a parent's consent goes by its form, and is taken back"

    stop_serve
    echo "$who: the logs"
    clean "$work/serve.log"
    # The reading went to the provider, which refused the key: no model read it.
    jq -se 'any(.[]; .message == "ai_usage" and .method == "/novatrail.api.v1.AttemptService/SubmitWork" and .ai_outcome == "key_refused")' "$work/serve.log" > /dev/null ||
        die "serve.log has no line ai_usage of SubmitWork with the outcome key_refused"
    ! jq -se 'any(.[]; .message == "ai_usage" and .ai_outcome == "answered")' "$work/serve.log" > /dev/null ||
        die "a model answered a call of the check"

    # The job after a rollout, on the database itself (R51): it asks what is open on the versions
    # the image no longer gives, of the pinned release, and closes it.
    echo "$who: after"
    API_PACKAGES_DIR="$work/packages" "$binary" after > "$work/after.log" 2>&1 || die "after failed: $(cat "$work/after.log")"
    clean "$work/after.log"
    echo "$who: ok"
}

# app_done ends the run for app: the server stops, the token goes — a token that would outlive its
# hour is not left behind, nor one half laid —, and the server's log is checked as the check's is,
# for the custom token too; a log that fails it fails the run.
app_done() {
    local failed=0
    stop_serve
    [[ -z ${sleep_pid:-} ]] || kill "$sleep_pid" 2> /dev/null || true
    if [[ -n ${laying:-} ]]; then
        if [[ -f $token_dir/custom-token && -f $work/serve.log ]] && grep -qF -- "$(cat "$token_dir/custom-token")" "$work/serve.log"; then
            echo "dev: serve.log holds the custom token" >&2
            failed=1
        fi
        rm -f -- "$token_dir/custom-token" "$token_dir"/.custom-token.*
    fi
    if [[ -f $work/serve.log ]]; then
        if (clean "$work/serve.log" "$app_uid"); then echo "dev-app: the log is clean"; else failed=1; fi
    fi
    rm -rf "$work"
    ((failed == 0)) || exit 1
}

# app keeps api up for the local build of app, whose container cannot run this script (R67; R146
# spec): the server on the stub of tutor on port 8080, on the packages of the pinned release, the
# account dev-app with its owner's profile, and a custom token of that account laid anew every half
# hour in the folder of the volume both devcontainers mount, until the server stops. The token is
# never printed.
app() {
    local port
    # Not local: app_done reads them when the script exits.
    app_uid=${API_DEV_APP_UID:-$app_uid_default}
    token_dir=${API_DEV_TOKEN_DIR:-$token_dir_default}
    who=dev-app
    token_folder "$token_dir"
    stubbed
    work=$(mktemp -d)
    trap app_done EXIT
    echo "dev-app: build"
    build
    migrated
    packaged
    reopened

    # The server's log goes to the terminal as well, as `just dev serve` shows it.
    echo "dev-app: serve, on the stub of tutor"
    API_PACKAGES_DIR="$work/packages" PORT=8080 "$binary" serve > "$work/serve.log" 2>&1 &
    serve_pid=$!
    tail -f --pid="$serve_pid" "$work/serve.log" &
    port=$(started "$work/serve.log")
    base="http://127.0.0.1:$port"

    echo "dev-app: ID token"
    token=$(id_token "$app_uid")
    legal_texts
    owner_account

    # From here the file is this run's: one that failed to start, beside a run that goes on, leaves
    # that run's token where it is.
    laying=1
    lay_token "$token_dir" "$app_uid"
    echo "dev-app: app signs in by the custom token in $token_dir/custom-token, laid anew every half hour; api is at http://$(hostname -i | cut -d' ' -f1):$port; stop with Ctrl-C"
    while :; do
        sleep "$token_every" &
        sleep_pid=$!
        # Whichever ends first: the half hour, or the server.
        wait -n "$sleep_pid" "$serve_pid" || true
        kill -0 "$serve_pid" 2> /dev/null || die "serve stopped"
        lay_token "$token_dir" "$app_uid"
    done
}

# Sourced by the test, the script defines its functions and runs nothing.
[[ ${BASH_SOURCE[0]} == "$0" ]] || return 0

load
case ${1:-} in
check)
    check
    ;;
app)
    app
    ;;
run)
    shift
    stubbed
    build
    exec "$binary" "$@"
    ;;
run-ai)
    shift
    modelled
    build
    exec "$binary" "$@"
    ;;
*)
    die "usage: dev.sh run <command of api> | dev.sh run-ai <command of api> | dev.sh check | dev.sh app"
    ;;
esac
