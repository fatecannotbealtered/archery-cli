# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [1.0.8] - 2026-06-17

### Fixed

- **2FA login now works against real Archery.** Session-mode login posted the OTP to `/api/v1/user/2fa/` (the 2FA *config* endpoint) instead of `/api/v1/user/2fa/verify/`, and never replayed the temp `session_key` from `/authenticate/` as the `sessionid` cookie — so the verify view could not find the password-verified user and rejected every code as wrong/expired, even when correct. The OTP is now posted to `/api/v1/user/2fa/verify/` with the temp session replayed as the `sessionid` cookie, matching Archery's real handshake. Verified live against `hhyo/archery:v1.8.5` (correct code logs in and caches the session; a wrong code returns the server's `验证码不正确！`). The prior unit test was self-fulfilling (its mock accepted exactly what the client sent); it now faithfully encodes the real contract — correct endpoint plus replayed session cookie — so a regression on either half fails the test.

## [1.0.7] - 2026-06-16

### Fixed

- npm `optionalDependencies` platform-package pins now match the package version. The previous release bumped the top-level version but left the pins at the prior version, so `npm install` resolved a stale platform binary (the new wrapper with the old binary). The publish workflow now rewrites `optionalDependencies` from the package version before `npm publish`, so the pins can no longer drift from the single source of truth.

## [1.0.6] - 2026-06-16

### Changed

- `update` now verifies the release Sigstore signature on `checksums.txt` **in-process** (embedded `sigstore-go`, bootstrapped from the embedded TUF trust root) instead of shelling out to an external `cosign`. Verification is mandatory and fail-closed: a missing signature bundle, a signature that does not verify against this repo's tagged release-workflow identity, or a checksum mismatch all refuse the update — there is no skip path. Releases are now signed with `cosign sign-blob --new-bundle-format`.

### Security

- Release-integrity failures (missing/invalid signature or checksum mismatch) now return the non-retryable `E_INTEGRITY` error code (exit 1) instead of a retryable network code, so an agent treats a possible supply-chain issue as a hard stop rather than retrying.

## [1.0.5] - 2026-06-16

### Added

- **`--read-only` global switch (and `ARCHERY_CLI_READONLY` env var).** When set, every write command is refused at the shared write chokepoint with an `E_FORBIDDEN` envelope (exit 4) — before the dry-run preview or any network call. Read commands are unaffected. This lets an agent run safely against production with writes hard-disabled regardless of `--dry-run`/`--confirm`. `doctor` and `context` report the current read-only state.
- **2FA detection + `--otp` (and `ARCHERY_CLI_OTP`).** Session login now understands Archery's two-factor flow. If an account has 2FA enabled, `/authenticate/` accepts the password but does not log in (it returns a `session_key` in `data` with no `sessionid` cookie); the CLI now detects this:
  - With `--otp <6-digit code>` it completes the login via `POST /api/v1/user/2fa/` and caches the resulting session, so **subsequent commands need no OTP** until the session expires.
  - Without an OTP it fails fast with the new **`E_2FA_REQUIRED`** code (exit 9, "human required") and a clear hint to retry with `--otp`, instead of the old opaque "session login failed".
  - 2FA codes are ~30s-lived: generate the code immediately before running. The cached `sessionid` (keyring) is reused afterwards, so OTP is a one-time cost per session.

### Changed

- **`workflow detail` reworked to surface execution results.** Detail now reads the execution/review result rows from `/sqlworkflow/detail_content/` and exposes them as `result[]` (each row carries `stage`, `stageStatus`, `errLevel`, `error`, `affectedRows`, `executeTime`), plus the string `statusCode` (e.g. `workflow_exception`) backfilled from the workflow list. An agent can now see **why an execution failed** (e.g. `Invalid remote backup information`) straight from the CLI instead of opening the web UI. SQL content is reconstructed from the rows, with inception wrapper statements filtered out.
- **Unified instance/group flags for `workflow submit` / `sqlcheck` / `auto-review`.** Session mode now also accepts the numeric `--instance` (ID) and `--group` (ID), resolving them to names via the instance/resource-group lists — the same flags JWT mode already used, so agents no longer have to remember a different flag set per transport. The name flags (`--instance-name` / `--group-name`) still work and win when both are given; an unknown ID fails with a clear `E_NOT_FOUND`.
- **Skill schema-discovery guidance.** The Skill now steers agents to locate tables/columns by **meaning** via `dict tables` / `dict table-info` (which carry table and column comments) instead of `instance resource` / `instance describe` (bare names only), with an explicit "stop guessing, ask the user" rule when no matching column exists. Fixes all `dict` examples that omitted the required `--db-type`, which fail with `Instance.DoesNotExist` on Archery v1.8.5.

### Notes

- **DDL execution and backups.** Executing a DDL workflow with backup enabled needs Archery's backup feature configured (`enable_backup_switch` on and a reachable backup database). On environments without it, execution fails with `Invalid remote backup information` — this is an Archery configuration prerequisite, not a CLI bug. Either have a DBA configure the backup database, or submit with backup disabled (`--backup=false`).

## [1.0.4] - 2026-06-16

### Added

- **Dual-mode transport — default `session`, REST `jwt` optional.** Login and every request now run over Archery's web (session + AJAX) endpoints by default, so a **plain developer account** can drive the CLI. REST + JWT is kept as an opt-in advanced mode via `--mode jwt` (a persistent flag) or a region's `mode` config. Mode precedence is `--mode` flag → region config → `session` default.
  - **Why:** Archery's `/api/v1` REST surface is closed to ordinary accounts (`403`) and is not enabled on older or most real-world deployments. The web session path (`/authenticate/`, `/sqlworkflow_list/`, `/sqlquery/`, …) works on **all versions and for ordinary accounts**, which is what most users actually have.
  - **Live-verified** against a local `hhyo/archery:v1.8.5` container with an ordinary (`cli_verify`) account: commands that return `403` under REST — `workflow list`, `instance list`, `user list`, `query log`, and others — all return `200` with real data over the session path.
- **New operational commands** (learned from real Archery ops Skills), all supported on the session path:
  - `instance create-db` — create a database on an instance.
  - `instance create-user` — create a database account.
  - `instance grant` — grant/revoke privileges (`--op grant|revoke`).
  - `instance test-instance` — connectivity probe.
  - `user resourcegroup-add` — add users/instances to a resource group.
  - `workflow audit-list` — list workflows awaiting audit.
  - `workflow auto-review` — classify SQL by the auto-review rules and optionally approve compliant workflows.

### Changed

- All 54 existing business commands now resolve over the session path by default; the JWT/REST path remains available unchanged via `--mode jwt`.

## [1.0.3] - 2026-06-15

### Added

- **Batch operations (CLI-SPEC §15).** Write workflows that act on many objects at once now run as a single agent-facing command — one envelope, one confirm token over the whole resolved set, one aggregated `items[]` + `summary{total,succeeded,failed,skipped}` — instead of a loop the agent has to drive:
  - `query run --instances a,b,c` runs one SQL across many instances, grouped per instance, failing soft (`--continue-on-error`, default `true`). The legacy `--instance` singular flag is kept as a compatibility alias.
  - `instance import --file <csv|json>` batch-onboards instances from a manifest (format inferred from the extension or `--manifest-format`), creating each via the existing single-create payload.
  - `workflow audit --ids 1,2,3` and `workflow execute --ids 1,2` batch-process workflows. `execute` is more conservative than the generic contract: the `--dangerous` gate is required and `--continue-on-error` defaults to `false` (stop at the first failure, report the unattempted remainder as `skipped`).
  - All four are **class B** client-side loops — Archery's upstream has no native bulk write endpoint, so results are **not** atomic and partial failures do not roll back already-applied items. The external contract (plural inputs, dry-run summary, single single-use confirm token, dangerous gate, per-item aggregation) is identical to a native batch.
  - `reference` gains a `batch_result` output schema plus runnable plural-input examples for each batch command.

### Fixed

- **`workflow execute` sent an incomplete payload.** It POSTed only `{workflow_id, mode}`, but Archery's `ExecuteWorkflowSerializer` requires `workflow_type` and — for SQL-review workflows — `engineer`, so every execute was rejected with `workflow_type 该字段是必填项`. The request now carries `workflow_type` (default `2`, SQL上线申请) and `engineer` (the authenticated user); both the single-target and `--ids` batch paths are fixed. Verified live on a real Archery stack (workflows reached `workflow_finish`).

### Changed

- npm scope 迁移 `@fatecannotbealtered-` → `@fateforge`（无横线 org 在 npm 被占，迁移到 `@fateforge`）. Updated the root package name, all platform `optionalDependencies`, the lockfile, the `update` command's `updateNPMPackage` constant, the README install commands and npm badges, and the platform-package generation script. The GitHub org / Go module path (`github.com/fatecannotbealtered/...`) and the `npx skills add fatecannotbealtered/...` source are unchanged.

## [1.0.2] - 2026-06-14

### Added

- `reference` now exposes a real per-command `output_schema` (label → fields/untrusted catalog) and a runnable `examples[]`, guarded against regression.

### Changed

- Confirm tokens are now single-use: replaying a confirmed write's token returns `E_CONFLICT` (safe-retry).

## [1.0.1] - 2026-06-14

### Added

- Offset-paginated list commands (`archive`, `slowquery`, `query`) now return an explicit `offset` echo and a `next_offset` cursor (present only when more rows remain), so an agent can page deterministically instead of re-deriving `offset + count` from `has_more`.

### Fixed

- Corrected `PrintErrorJSON`/`PrintErrorJSONWithCode` docstrings: the JSON failure envelope is emitted on **stdout** (the single document agents parse, per CLI-SPEC §4), which the code already did — only the comments wrongly said stderr.

## [1.0.0] - 2026-06-14

First stable release: recorded live smoke against a real Archery v1.8.5 stack (`docs/LIVE-SMOKE-EVIDENCE.md`); `release_readiness` is now `stable` with `live_smoke_status: verified`.

### Fixed
- **Session-mode commands never authenticated.** `dict`, `diagnostic`, `query`, `slowquery`, `binlog`, and `archive` use Archery's legacy Django endpoints, which need a session cookie — but the client only sent a JWT Bearer, so every request was redirected to `/login/` and the HTML page was parsed as JSON. `LoginWithSession` existed but was never called. Now `ensureSession` performs the Django form login lazily (GET `/login/` for the csrftoken, POST `/authenticate/` with `csrfmiddlewaretoken` + credentials), wired into `internalRequest`, and unsafe methods carry the `X-CSRFToken` header. Credentials come from the resolved region (env vars when the keyring holds only a JWT). Found by live smoke.
- **`instance create` sent `type` as an integer.** Archery's `Instance.type` is a `CharField` with choices `('master','slave')`; the API rejected `type 0`. Now sends the string.
- **`instance_tag` typed as `string`.** It is a ManyToMany relation serialized as an array, so `instance list`/`describe` failed to parse against a real server. Now `[]any`.

### Added
- Boundary test suite (`test/e2e`): all 55 leaf commands now execute through the real binary against a universal mock Archery upstream — read commands assert the ok envelope, write commands assert the dry-run `confirm_token`, plus full dry-run→confirm cycle, cross-machine token rejection, missing-credential, and usage-error paths.
- Command-level `update --check` test against a mock GitHub releases API.
- FCC enumeration guard (`TestFCC_EveryLeafCommandHasTest`): enumerates every leaf command from live `reference` output and asserts each has a command-level test; skips while `fcc_status` is honestly declared non-verified, so the claim cannot be flipped without coverage.
- Initial unreleased implementation.
- Multi-region support (cn/overseas).
- JWT authentication via /api/auth/token/.
- Workflow management (list, submit, detail, audit, execute, cancel, sqlcheck).
- SQL query execution and management.
- Instance management (CRUD, resources, describe, users).
- Slow query analysis and optimization.
- Database diagnostics (process, kill, tablespace, locks, transactions).
- Binlog management (list, parse, purge).
- Data archiving (list, apply, audit, switch, once, log).
- Data dictionary (tables, views, triggers, procedures, export).
- User and group management.
- Self-description commands (reference, doctor, context, changelog, update).
- Audit logging for write operations.
- _untrusted tagging for external content.
- npm wrapper distribution scaffolding.
- Agent-facing conformance checks for write confirmation, risk metadata, ID normalization, and URL validation.
- Repository governance files for AI-native open-source distribution: `AGENTS.md`, `NOTICE.md`, `CODE_OF_CONDUCT.md`, `docs/E2E.md`, and Dependabot configuration.
- npm lockfile and CI npm audit coverage.

### Changed
- Synced `.agent/` SEC-SPEC from the template: credential-at-rest is now the keyring three-part pattern (password discarded after login / secrets in the OS keyring / zero-secret config), file encryption demoted to a visible fallback, env vars as the recommended secret channel, and an honest note on Windows `0600` semantics.
- `release_readiness` is back to `beta` with `fcc_status: verified` — now machine-backed by the enumeration guard over the new boundary suite instead of hand-declared.
- `release_readiness` now declares `unpublishable` with `fcc_status: missing`: existing cmd tests inspect cobra flag definitions instead of executing commands, so CLI-boundary coverage is absent. `doctor` reports the matching `fail` with an actionable fix.
- In JSON mode the failure envelope is now the single JSON document on stdout, matching CLI-SPEC §4; stderr stays a human-readable side channel.
- Synced the `.agent/` spec copies from the ai-native-cli-spec template: stdout failure envelope (§4), HMAC confirm-token requirement (§7), signature_status/signature_verified fields (§14), Skill frontmatter `version` rule.
- Unified the golangci-lint v2 toolchain: Makefile installs from the `/v2` module path and CI uses `golangci-lint-action@v8` to match the v2 config format.
- Write commands now consistently require `--dry-run` followed by `--confirm <confirm_token>`, including authentication, query execution/favorite, workflow, instance, archive, binlog, diagnostic, and self-update writes.
- `auth login` now uses explicit `--url` in non-interactive JSON mode and validates HTTPS by default, allowing HTTP only for loopback development URLs.
- Agent-facing JSON output normalizes IDs to strings and tags common external/generated content fields with `_untrusted`.
- Self-update now syncs the whole Agent Skill directory through `npx skills add fatecannotbealtered/archery-cli -y -g` and reports `skill_sync_status`.
- Skill, README, `.agent/` specs, and reference docs now point agents to `reference` as the machine truth and document the current confirmation flow.

### Security
- Confirm tokens are now signed with a machine-local HMAC key (`confirm.secret`, created on first use with 0600 permissions) so they cannot be fabricated without running `--dry-run` on the same machine.
- Confirm tokens bind command path, operation payload, region, and username context; dry-run previews redact secrets while confirmation tokens bind the full payload.
- High and critical write commands now require explicit `--dangerous` in both dry-run and confirm steps as the T2 second gate.
- Release checksums are signed with Sigstore/Cosign, and install/update paths report signature verification status separately from checksum verification.
- npm installer checksum verification hard-fails when integrity cannot be verified.
- Credential persistence now uses OS keyring-only storage; passwords and tokens are never written to the config file.

### Fixed

- `update` text output now reads the snake_case result fields, so version numbers render instead of empty strings; removed duplicated camelCase keys (`currentVersion`, `targetVersion`, `updateAvailable`, `releaseUrl`, `installMethod`, `pendingPath`) from the top-level update JSON in favor of the snake_case set.

