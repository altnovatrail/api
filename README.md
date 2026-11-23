# api

[![CI](https://github.com/altnovatrail/api/actions/workflows/ci.yml/badge.svg)](https://github.com/altnovatrail/api/actions/workflows/ci.yml)
[![Release](https://github.com/altnovatrail/api/actions/workflows/release.yml/badge.svg)](https://github.com/altnovatrail/api/actions/workflows/release.yml)
[![CodeQL](https://github.com/altnovatrail/api/actions/workflows/codeql.yml/badge.svg)](https://github.com/altnovatrail/api/actions/workflows/codeql.yml)
[![Latest release](https://img.shields.io/github/v/release/altnovatrail/api)](https://github.com/altnovatrail/api/releases/latest)
[![Go version](https://img.shields.io/github/go-mod/go-version/altnovatrail/api)](go.mod)
[![Coverage](https://codecov.io/gh/altnovatrail/api/branch/main/graph/badge.svg)](https://codecov.io/gh/altnovatrail/api)
[![OpenSSF Best Practices](https://www.bestpractices.dev/projects/16661/badge)](https://www.bestpractices.dev/projects/16661)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/altnovatrail/api/badge)](https://scorecard.dev/viewer/?uri=github.com/altnovatrail/api)
[![License](https://img.shields.io/github/license/altnovatrail/api)](LICENSE)

[![Code Smells](https://sonarcloud.io/api/project_badges/measure?project=altnovatrail_api&metric=code_smells)](https://sonarcloud.io/summary/new_code?id=altnovatrail_api)
[![Reliability Rating](https://sonarcloud.io/api/project_badges/measure?project=altnovatrail_api&metric=reliability_rating)](https://sonarcloud.io/summary/new_code?id=altnovatrail_api)
[![Security Rating](https://sonarcloud.io/api/project_badges/measure?project=altnovatrail_api&metric=security_rating)](https://sonarcloud.io/summary/new_code?id=altnovatrail_api)
[![Maintainability Rating](https://sonarcloud.io/api/project_badges/measure?project=altnovatrail_api&metric=sqale_rating)](https://sonarcloud.io/summary/new_code?id=altnovatrail_api)
[![Vulnerabilities](https://sonarcloud.io/api/project_badges/measure?project=altnovatrail_api&metric=vulnerabilities)](https://sonarcloud.io/summary/new_code?id=altnovatrail_api)

The NovaTrail platform service. It checks the account owner's sign-in and keeps the learners, their consent and their attempts in its database — among the platform's parts, the one place where learners' data is stored on the server, the learner model with it, as an append-only log of evidence. It runs each check of a learner's work, weighs the evidence of the attempt and picks the next lesson or task with a reason code.

A component of [NovaTrail](https://github.com/altnovatrail/spec), a universal open platform for adaptive learning, interactive lessons and AI tutoring. Its place in the platform:

- app, on the learner's device, calls api, by the contract that spec holds;
- api checks the sign-in with the sign-in service and keeps its data in its own database;
- tutor, module-kit, learner-model and engine are libraries in api's process: tutor reads the work and words the tutor's replies, module-kit gives the verdict by the module, learner-model weighs the evidence, and engine chooses what comes next;
- AI providers are reached only through tutor, which sees the work but not whose it is;
- the modules come from a pinned release of modules, whose packages api holds and whose device files it serves to app.

Its card is the one named `api` in spec's [docs/architecture.md](https://github.com/altnovatrail/spec/blob/main/docs/architecture.md#35-api--the-platforms-service); the detail, the decisions and how to build and run it are in [docs/](docs/README.md).
