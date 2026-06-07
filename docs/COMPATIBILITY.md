# Compatibility Matrix

archery-cli is a CLI wrapper for the [Archery](https://github.com/hhyo/Archery) SQL audit platform. This document tracks verified compatibility.

## Archery Platform

| Archery Version | Status | Notes |
|-----------------|--------|-------|
| 1.11.x | Verified | Primary development target |
| 1.10.x | Compatible | Most features supported |
| 1.9.x  | Partial  | Some internal endpoints may differ |

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
| 1.22+ | Required (for generics support) |
