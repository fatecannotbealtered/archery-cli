# Compatibility Matrix

archery-cli is a CLI wrapper for the [Archery](https://github.com/hhyo/Archery) SQL audit platform. This document tracks verified compatibility.

## Archery Platform

| Archery Version | Status | Notes |
|-----------------|--------|-------|
| 1.14.x | Verified | 2FA (TOTP) login path live-verified 2026-09-07; see docs/LIVE-SMOKE-EVIDENCE.md |
| 1.11.x | Verified | Primary development target |
| 1.10.x | Compatible | Most features supported |
| 1.9.x  | Verified | Submit/sqlcheck moved to the REST endpoints for this version and later; see below |
| 1.8.5  | Verified | The e2e reference stack |

### The v1.9.0 route removal

Archery deleted two root-level AJAX routes in **v1.9.0**, together with the
views behind them in `sql/sql_workflow.py`:

| Removed route | Was used by | Replacement |
|---------------|-------------|-------------|
| `POST /autoreview/` | `workflow submit` | `POST /api/v1/workflow/` |
| `POST /simplecheck/` | `workflow sqlcheck`, `workflow auto-review` | `POST /api/v1/workflow/sqlcheck/` |

Against a v1.9.0+ server the old routes return **404** — which reads like a bad
parameter but is the server missing the endpoint, so retrying with a different
instance, resource group or SQL cannot help.

Both replacements also exist in v1.8.5, so the CLI now uses them on every
version and in both transport modes. They need no JWT: Archery lists
`SessionAuthentication` in its DRF `DEFAULT_AUTHENTICATION_CLASSES`, and both
endpoints override the global API-whitelist permission with `IsAuthenticated`.

Every other session route the CLI uses survived v1.9.0 — `/passed/`,
`/execute/`, `/cancel/`, `/sqlworkflow_list/`, `/sqlworkflow/detail_content/`,
and the `/instance/`, `/query/`, `/db_diagnostic/`, `/slowquery/`, `/binlog/`,
`/archive/`, `/data_dictionary/` families.

## API Versions

| API | Endpoint Pattern | Status |
|-----|-----------------|--------|
| REST v1 | `/api/v1/...` | Verified |
| Internal | `/instance/...`, `/sqlworkflow/...`, `/binlog/...`, `/archive/...` | Verified |

## Supported Database Types

archery-cli passes `--db-type` through to Archery. Supported types depend on the Archery server configuration and installed goSQL plugins.

| Database | `--db-type` Value | Status |
|----------|-------------------|--------|
| MySQL | `mysql` | Verified |
| PostgreSQL | `pgsql` | Verified |
| Microsoft SQL Server | `mssql` | Compatible |
| Redis | `redis` | Compatible |
| ClickHouse | `clickhouse` | Compatible |
| Oracle | `oracle` | Compatible |
| MongoDB | `mongo` | Compatible |

> **Note**: "Compatible" means the CLI passes the type through correctly; actual support depends on the Archery server's goSQL plugin configuration.

## Platform

| OS | Status |
|----|--------|
| Linux (x64, arm64) | Verified |
| macOS (x64, arm64) | Verified |
| Windows (x64) | Verified |

## Go Version

| Go Version | Status |
|------------|--------|
| Version declared in `go.mod` | Required for local builds and CI |
