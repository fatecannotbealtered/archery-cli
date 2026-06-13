# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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

