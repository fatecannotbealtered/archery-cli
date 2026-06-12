# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
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

