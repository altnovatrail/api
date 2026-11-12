#!/usr/bin/env bash
# The run of api on the dev projects of Firebase and Neon (R39): `just dev <command>` runs a command
# of the binary with the configuration of the dev file, and `just dev-check` goes the whole way —
# migrations, an invite code, a country, the packages of modules, the server, an ID token, the legal
# texts, the account, the owner's profile, its progress and "something else", a parent's consent by
# its form, then the job after a rollout — and checks the answers and the logs.
#
# The configuration is KEY=VALUE lines in a file outside any git working tree, by default in the
# volume the devcontainer mounts (R39): its lines are read as they are, never run, only names of
# api's own, API_*, are taken, and no value of it is printed. What the binary does not read — the
# Web API key, the UID of the dev account — has names of the recipe's own, API_DEV_*, which the
# binary ignores. No secret goes on a command line, where any process could read it: they go
# through the environment, files and pipes.
#
# scripts/dev_test.go tests the loader and the custom token in CI.
set -euo pipefail
shopt -s inherit_errexit

config=${API_DEV_CONFIG:-/home/vscode/.novatrail-dev/dev.env}
# The account of the dev project is a test one, by a UID of its own (R39).
uid_default=dev-author
# Synthetic, as the account is.
country=US
# The binary both recipes run, built as `just build` builds it; bin/ is ignored by git.
binary=bin/api

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

# b64url is base64url with no padding, as a JWT writes its parts.
b64url() { openssl base64 -A | tr '+/' '-_' | tr -d '='; }

# custom_token prints a custom token of the dev account, signed by RS256 with the service account's
# key in the file $1: the claims are Firebase's for a custom token made with a JWT library (R39).
# The key reaches openssl through a pipe.
custom_token() {
    local email now header payload unsigned signature
    email=$(jq -er .client_email "$1") || die "the key has no client_email"
    now=$(date +%s)
    header=$(printf '{"alg":"RS256","typ":"JWT"}' | b64url)
    payload=$(EMAIL=$email UID_=${API_DEV_UID:-$uid_default} NOW=$now jq -cn '{
        iss: env.EMAIL, sub: env.EMAIL,
        aud: "https://identitytoolkit.googleapis.com/google.identity.identitytoolkit.v1.IdentityToolkit",
        iat: (env.NOW | tonumber), exp: ((env.NOW | tonumber) + 3600), uid: env.UID_}' | b64url)
    unsigned="$header.$payload"
    signature=$(printf '%s' "$unsigned" | openssl dgst -sha256 -sign <(jq -er .private_key "$1") | b64url) ||
        die "the key would not sign the custom token"
    printf '%s.%s\n' "$unsigned" "$signature"
}

# id_token prints an ID token of the dev account: its custom token exchanged at the sign-in service
# (R39). The Web API key reaches curl through its configuration on stdin, the custom token through
# the environment and a pipe.
id_token() {
    local key=${API_FIREBASE_CREDENTIALS_FILE:-} custom
    [[ -n $key && -f $key ]] || die "API_FIREBASE_CREDENTIALS_FILE must name the service account's key"
    [[ -n ${API_DEV_WEB_API_KEY:-} ]] || die "API_DEV_WEB_API_KEY must be set: the Web API key of the dev project"
    custom=$(custom_token "$key")
    printf 'url = "https://identitytoolkit.googleapis.com/v1/accounts:signInWithCustomToken?key=%s"\n' "$API_DEV_WEB_API_KEY" |
        curl -sS --fail -K - -H 'Content-Type: application/json' \
            -d @<(TOKEN=$custom jq -cn '{token: env.TOKEN, returnSecureToken: true}') |
        jq -er .idToken || die "the sign-in service did not exchange the custom token"
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
# must not: the UID, an email, a connection string, the Web API key, the token (R09 spec).
clean() {
    jq -e . "$1" > /dev/null || die "a line of $(basename "$1") is not JSON"
    ! grep -qF -- "${API_DEV_UID:-$uid_default}" "$1" || die "$(basename "$1") names the UID"
    ! grep -qE '[[:alnum:]._%+-]+@[[:alnum:].-]+\.[[:alpha:]]{2,}' "$1" || die "$(basename "$1") holds an email"
    local name value
    for name in API_DATABASE_URL API_OPENFGA_DATABASE_URL API_MIGRATE_DATABASE_URL API_MIGRATE_OPENFGA_DATABASE_URL \
        API_DEV_WEB_API_KEY; do
        value=$(secret "$name")
        [[ -z $value ]] || ! grep -qF -- "$value" "$1" || die "$(basename "$1") holds $name"
    done
    [[ -z ${token:-} ]] || ! grep -qF -- "$token" "$1" || die "$(basename "$1") holds the token"
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

check() {
    work=$(mktemp -d)
    trap 'stop_serve; rm -rf "$work"' EXIT
    echo "dev-check: build"
    build

    # What the dev file declares for admission, checked before the migrations as the rollout of
    # infra checks a deployment's (R45).
    echo "dev-check: check"
    "$binary" check > "$work/check.log" 2>&1 || die "check failed: $(cat "$work/check.log")"
    clean "$work/check.log"

    echo "dev-check: migrate"
    "$binary" migrate > "$work/migrate.log" || die "migrate failed: $(cat "$work/migrate.log")"
    clean "$work/migrate.log"
    echo "dev-check: allow $country"
    "$binary" countries allow "$country" > /dev/null

    # The packages of the pinned release, laid afresh for this run, as the image's build lays them
    # (R41): a folder of the dev file stays as the author laid it.
    echo "dev-check: packages"
    "$binary" packages "$work/packages" > "$work/packages.log" || die "packages failed: $(cat "$work/packages.log")"
    clean "$work/packages.log"

    echo "dev-check: serve"
    mkdir -p "$work/forms"
    API_PACKAGES_DIR="$work/packages" API_CONSENT_FORMS_DIR="$work/forms" PORT=0 "$binary" serve > "$work/serve.log" 2>&1 &
    serve_pid=$!
    local port=""
    for _ in $(seq 60); do
        port=$(jq -r 'select(.message == "startup") | .port' "$work/serve.log" 2> /dev/null || true)
        [[ -n $port ]] && break
        kill -0 "$serve_pid" 2> /dev/null || die "serve did not start: $(cat "$work/serve.log")"
        sleep 0.5
    done
    [[ -n $port ]] || die "serve did not start in 30 s"
    base="http://127.0.0.1:$port"

    echo "dev-check: ID token"
    token=$(id_token)

    # The legal texts, which go with no token, and a new account names the current versions of the policy
    # and the terms (R52; R130, R136 spec).
    call LegalService/GetLegalTexts <<< '{}'
    expect GetLegalTexts
    jq -e '[.texts[]?.id] | index("privacy-policy") != null and index("terms-of-use") != null' "$work/answer.json" > /dev/null ||
        die "GetLegalTexts gave $(jq -c '[.texts[]?.id]' "$work/answer.json"), want the policy and the terms"
    cp "$work/answer.json" "$work/texts.json"
    echo "dev-check: the legal texts are there"

    call GetAccount <<< '{}'
    # A UID with no account is refused as permission_denied, CAUSE_NO_ACCESS (R04).
    if [[ $status == 403 ]] && [[ $(jq -r .code "$work/answer.json") == permission_denied ]]; then
        echo "dev-check: no account yet: an invite code and CreateAccount"
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
        echo "dev-check: no owner's profile yet: CreateOwnerProfile"
        call CreateOwnerProfile < <(COUNTRY=$country jq -cn '{country: env.COUNTRY, settings: {pseudonym: "Dev"}}')
        expect CreateOwnerProfile
        call GetAccount <<< '{}'
        expect GetAccount
    fi
    jq -e '(.account.account_id // "") != "" and
        ([.account.profiles[]? | select(.owner and .age_category == "adult")] | length) == 1' \
        "$work/answer.json" > /dev/null || die "GetAccount does not hold the account with the owner's profile, adult"
    echo "dev-check: the account and the owner's profile are there"

    # The progress of the owner's profile settles its evidence first, under the lock of its row of
    # the cache, on the database itself (R49).
    local owner
    owner=$(jq -r '.account.profiles[] | select(.owner) | .profile_id' "$work/answer.json")
    call ProgressService/GetProgress < <(PROFILE=$owner jq -cn '{profile_id: env.PROFILE}')
    expect GetProgress
    echo "dev-check: the progress of the owner's profile is there"

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
    echo "dev-check: olympiad-math is connected, and the engine gives a $(jq -r '.choice.kind | ltrimstr("CHOICE_KIND_") | ascii_downcase' "$work/answer.json")"

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
    echo "dev-check: a parent's consent goes by its form, and is taken back"

    stop_serve
    echo "dev-check: the logs"
    clean "$work/serve.log"

    # The job after a rollout, on the database itself (R51): it asks what is open on the versions
    # the image no longer gives, of the pinned release, and closes it.
    echo "dev-check: after"
    API_PACKAGES_DIR="$work/packages" "$binary" after > "$work/after.log" 2>&1 || die "after failed: $(cat "$work/after.log")"
    clean "$work/after.log"
    echo "dev-check: ok"
}

# Sourced by the test, the script defines its functions and runs nothing.
[[ ${BASH_SOURCE[0]} == "$0" ]] || return 0

load
case ${1:-} in
check)
    check
    ;;
run)
    shift
    build
    exec "$binary" "$@"
    ;;
*)
    die "usage: dev.sh run <command of api> | dev.sh check"
    ;;
esac
