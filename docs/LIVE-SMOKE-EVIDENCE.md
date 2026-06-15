# Live Smoke Evidence

Recorded live smoke for `release_readiness.required_evidence:
recorded_live_smoke_for_stable`, run against a real Archery stack rebuilt
from scratch for this acceptance.

- **Date:** 2026-06-14
- **Archery:** `hhyo/archery:v1.8.5` + `mysql:5.7` + `redis:5` +
  `hanchuanchuan/goinception`, on `localhost:9123`. DB initialized fresh
  (43 tables via `makemigrations sql` + `migrate`), superuser created, the
  user added to Archery's `api_user_whitelist`.
- **Method:** each command invoked with `--format json`; envelope `ok`/`error`
  asserted. JWT cached in the OS keyring; session-mode commands authenticate
  via the Django form login.

## 2026-06-15 — batch commands (`--ids` / `--instances` / `--file`)

Smoke for the new client-side batch contract (CLI-SPEC §15) against the same
`hhyo/archery:v1.8.5` stack on `localhost:9123`, restarted for this run.

- **Method:** each batch command run with `--compact`; the aggregated envelope
  (`items[]` + `summary{total,succeeded,failed,skipped}`) and the per-item
  `error{code,message,retryable}` shape were asserted. No real query results,
  hostnames, credentials, or workflow contents are reproduced here.

### Result by command

| Command | Mode | Result |
|---|---|---|
| `query run --instances` | **live (read)** | PASS — real SELECT executed; partial-batch (1 good + 1 bad instance) aggregated `succeeded:1 / failed:1`; `--continue-on-error=false` marked the unattempted remainder `skipped`; per-item `error.code` populated |
| `workflow audit --ids` | live (reversible) | PARTIAL — batch path reaches the real audit API per item and aggregates failures; full pass blocked because Archery's audit serializer also requires `engineer`+`workflow_type` and no auditable workflow could be created (`workflow submit` against this stack's REST endpoint returns `workflow 该字段是必填项`). Dry-run shape + medium-tier (no `--dangerous`) verified |
| `workflow execute --ids` | **live (irreversible)** | PASS — upgraded from dry-run-only on 2026-06-15. Two audited-passed workflows seeded directly via Archery's ORM (status `workflow_review_pass` + matching `workflow_audit` `current_status=1`); batch executed with `--mode manual` (no SQL run on the DB, status → `workflow_finish`). Verified live: `--dangerous` gate, `confirm_token` single-use (replay → `E_CONFLICT`), `continue-on-error=false` stop-on-first-error (remainder `skipped`, untouched), `continue-on-error=true` continues past a failure, per-item `items[]/summary` aggregation. Required fixing a CLI defect first (see below). Cleaned up afterward |
| `instance import --file` | **live (irreversible)** | PASS — upgraded from dry-run-only on 2026-06-15. CSV (2 rows) + JSON (1 row) manifests imported live against `e2e-mysql`; per-row `create` aggregated `succeeded:2` then `succeeded:1`, per-item `data` returned. Verified live: `--dangerous` gate (dry-run without it → `E_CONFIRMATION_REQUIRED`), **password field absent from both preview and result envelopes**, `confirm_token` single-use (replay → `E_CONFLICT`). The 3 test instances deleted afterward; env left clean |

### Cross-cutting contract points (live-verified across `query run --instances`, `instance import --file`, `workflow execute --ids`)

- **Partial-failure aggregation** — `succeeded`/`failed`/`skipped` tally matches
  per-item `items[]`; no rollback of already-applied items (non-atomic, as
  documented).
- **Single-use confirm token** — first `--confirm` consumes the token; a second
  use of the same token is rejected with `E_CONFLICT` ("confirm token already
  used").
- **`--dangerous` gate** — high-risk batches (`query run`, `workflow execute`)
  refuse even `--dry-run` without `--dangerous`, returning
  `E_CONFIRMATION_REQUIRED`.
- **Plural-target dedup** — duplicate targets in `--instances` collapse to one
  resolved target (§15.1).

### Defects found and fixed by the 2026-06-15 batch run

1. **`query run` swallowed upstream errors as empty successes.** The result
   guard checked `error_message` (rarely set) instead of Archery's
   `{"status":1,"msg":"…"}`, so RBAC/param errors returned `ok:true` with
   `rows:null` — fatal for batch, which then reported false `succeeded`. Now
   surfaces `msg`, plus the per-query `data.error`.
2. **`query run` sent wrong form field names.** Archery's `/query/` view reads
   `sql_content`/`limit_num`/`tb_name`; the CLI sent `sql`/`limit`/`table_name`,
   so every query was rejected with "页面提交参数可能为空". Fixed to the view's
   names.
3. **`query run` read the row payload from the wrong level.** The result set is
   nested under `data` (`data.column_list`/`data.rows`); the struct read
   top-level. Now reads the nested object; `row_count` derived from `len(rows)`.
4. **`workflow audit` sent `remark` instead of `audit_remark`.** Archery's
   `AuditWorkflowSerializer` requires `audit_remark`; the mismatch left it empty
   and the audit was rejected. Fixed the JSON tag.

All four fixes verified live for `query run`; the `audit_remark` fix verified to
clear that specific validation error (audit still blocked by other required
fields, see table). `go test ./...` stays green after the changes.

### Defect found and fixed by the 2026-06-15 `workflow execute --ids` live run

5. **`workflow execute` sent an incomplete payload.** The CLI POSTed only
   `{workflow_id, mode}` to `/api/v1/workflow/execute/`, but Archery v1.8.5's
   `ExecuteWorkflowSerializer` requires `workflow_type` and — for SQL-review
   workflows (type 2) — `engineer`. The API rejected every execute with
   `workflow_type 该字段是必填项`, which is why the 2026-06-15-早 run could only
   reach dry-run. Fixed: `WorkflowExecuteRequest` now carries
   `workflow_type` (defaulted to `WorkflowTypeSQLReview = 2`) and `engineer`
   (the authenticated region username, sourced after `newClient`). Both the
   single-target and `--ids` batch paths updated. Verified live end-to-end
   (workflows reached `workflow_finish`); `go test ./...` stays green.

   *Data note:* the 2026-06-15-早 blocker ("no auditable workflow could be
   created via REST submit") was worked around by seeding two audited-passed
   workflows directly through Archery's own ORM in the isolated e2e container —
   `SqlWorkflow(status='workflow_review_pass')` + `SqlWorkflowContent` +
   `WorkflowAudit(current_status=1)` + an audit log, exactly the shape Archery's
   auto-review path produces. `--mode manual` was used so execute only flips
   status to `workflow_finish` and writes a log; **no SQL was run against the
   database.** All seeded workflows and the 3 test instances were deleted after
   the run.

## Result by command class

### REST / JWT (`/api/`) — PASS
| Command | Result |
|---|---|
| `auth login` (dry-run → confirm) | PASS — JWT cached, **password not written to disk** (keyring) |
| `auth status`, `context`, `doctor` | PASS |
| `user list`, `user groups` | PASS |
| `instance create` (T2 `--dangerous` double-gate + confirm; tampered token rejected) | PASS |
| `instance list`, `instance describe` | PASS |
| `workflow list` | PASS |

### Session mode (legacy Django endpoints) — PASS
The session-login path (GET `/login/` for csrftoken → POST `/authenticate/`)
plus the `X-CSRFToken` header on unsafe methods make these work:
| Command | Method | Result |
|---|---|---|
| `dict tables`, `dict table-info` | GET | PASS |
| `query log` | GET | PASS |
| `diagnostic process` | POST + CSRF | PASS |
| `diagnostic transactions` | POST + CSRF | PASS |

`diagnostic tablespace` returns `E_NOT_FOUND` — the endpoint is absent in
Archery v1.8.5; surfaced cleanly as a 404, not a crash.

## Defects found and fixed by this smoke run

1. **`instance create` sent `type` as an integer (0/1).** Archery's
   `Instance.type` is a `CharField` with choices `('master','slave')`; the API
   rejected `type 0`. Now sends the string. Mock tests accepted the int and
   never caught it.
2. **`instance_tag` typed as `string`.** It is a ManyToMany relation
   serialized as an array; `instance list`/`describe` failed with
   `cannot unmarshal array into ... string`. Now `[]any`.
3. **Session-mode commands never authenticated (architectural).**
   `dict`, `diagnostic`, `query`, `slowquery`, `binlog`, `archive` hit
   Archery's legacy Django endpoints, which need a session cookie — but the
   client only ever sent a JWT Bearer, so every such request was redirected to
   `/login/` and the HTML login page was parsed as JSON. `LoginWithSession`
   existed but was **never called**. Fixed by:
   - `ensureSession`: lazy Django form login (GET `/login/` to obtain the
     csrftoken cookie, POST `/authenticate/` with `csrfmiddlewaretoken` +
     credentials), wired into `internalRequest`;
   - `X-CSRFToken` header on non-GET internal requests (Django's CSRF
     middleware rejects unsafe methods without it — this was a second 403 on
     POST endpoints after the GET path already worked);
   - session credentials sourced from the resolved region (env vars when the
     keyring holds only a JWT); a clear error when neither is present.

## Note on archery's resource-group RBAC

Some operational reads (e.g. `slowquery review`) additionally require the
caller's group to be associated with the target instance via Archery's
resource groups. With an unassociated instance Archery returns
`{"status":1,"msg":"你所在组未关联该实例","data":[]}`; this is an Archery
authorization config, not a CLI defect.

## 2026-06-15 — `query` group, session path (v1.8.5)

Full live verification of the `query` command group against
`hhyo/archery:v1.8.5` on `localhost:9123` (= company production version), using
a superuser (`cli_verify`) in session mode (the default). Credentials supplied
via `ARCHERY_CLI_URL`/`USERNAME`/`PASSWORD`; each call performs the Django form
login (GET `/login/` → POST `/authenticate/`) then the AJAX request with
`X-CSRFToken`. No credentials, hostnames, or real query contents reproduced
here.

### Result by command

| Command | Mode | Result |
|---|---|---|
| `query log` | **live (read)** | PASS — offset envelope (`count`/`has_more`/`items`) and `total` agree; `sql`/`alias` tagged `_untrusted`; `--star` filter reflects favorite state. `username` is empty because v1.8.5's `_querylog` returns a blank `user_display` (server data, not a CLI defect). |
| `query explain` | **live (read)** | PASS — `EXPLAIN SELECT 1` returns the zipped plan (`plan[]` of `{col:val}`, `_untrusted`); a non-EXPLAIN statement now surfaces the server reason. Required a CLI fix (see below). |
| `query run` | **live (high-risk write)** | PASS — `--dangerous` gate refuses even `--dry-run` without it (`E_CONFIRMATION_REQUIRED`); dry-run emits `preview` + `confirm_token`; confirmed real `SELECT` returns `columns`/`rows`/`row_count` with `rows` tagged `_untrusted`. Batch `--instances` aggregates `items[]` + `summary`; text format renders. SELECT-only, no DB state changed. |
| `query favorite` | **live (write)** | PASS — dry-run preview + `confirm_token`; confirmed star (with `--alias`) visible via `query log --star` (`favorite:true`); unstarred afterward to leave the env clean (`--star` count back to 0). |
| `query generate` | **contract (gated)** | PASS — fails fast with `E_NOT_FOUND` (exit 3); confirmed server-side `POST /query/generate_sql/` returns HTTP 404 on v1.8.5, so the gate matches reality. |

### Defect found and fixed by this run

1. **`query explain` buried the server reason on rejection.** On a non-EXPLAIN
   statement the view returns `{"status":1,"msg":"仅支持explain开头的语句…","data":[]}`
   — `data` is an empty **array**, but the CLI decoded `data` straight into the
   `{column_list, rows}` object struct, so JSON unmarshal failed first and the
   user saw `cannot unmarshal array into … queryExplainDataSet` instead of the
   real message. Fixed by decoding `data` as `json.RawMessage` and binding it to
   the typed struct only **after** the `status` check passes (the
   `json.RawMessage` deferred-decode pattern already used by `dict`/`instance`/
   `workflow`). Success path (object) unchanged; rejection path now surfaces
   `msg`. Covered by `TestQueryExplainResponseDecode` (now exercises both the
   object success shape and the `data:[]` rejection envelope).

## 2026-06-15 — `workflow` group, session path (v1.8.5)

Full live verification of the `workflow` command group against
`hhyo/archery:v1.8.5` on `localhost:9123` (= company production version), using
a superuser (`cli_verify`) in session mode (the default). Credentials supplied
via `ARCHERY_CLI_URL`/`USERNAME`/`PASSWORD`; each call performs the Django form
login then the AJAX request with `X-CSRFToken`. No credentials, hostnames, or
real workflow contents reproduced here.

### Result by command

| Command | Mode | Result |
|---|---|---|
| `workflow list` | **live (read)** | PASS — `/sqlworkflow_list/` offset envelope (`items`/`count`/`total`/`has_more`); `--status`/`--search`/`--limit`/`--offset` applied; `title` tagged `_untrusted`; row `statusCode`/`status` mapped. |
| `workflow audit-list` | **live (read)** | PASS — `/sqlworkflow_list_audit/` same shape; empty set when nothing pending. |
| `workflow detail <id>` | **live (read)** | PASS — `/sqlworkflow/detail_content/`; SQL content reconstructed from the review rows, `sql`+`title` `_untrusted`. Metadata fields (title/db/created) are blank by design — v1.8.5's `detail_content` returns only review rows, no metadata endpoint exists (documented in `workflow.go`). |
| `workflow sqlcheck` | **live (read)** | PASS — `/simplecheck/` returns per-statement `{errlevel,level,message,stagestatus,affected_rows,sql}`; `SELECT` correctly rejected (DML/DDL only), clean DDL → `errlevel 0`, keyword DDL → `errlevel 1` warning. |
| `workflow auto-review` (classify) | **live (read)** | PASS — same `/simplecheck/` engine; clean DDL → `compliant:true blocked:0`, warning DDL → `compliant:false blocked:1` (`verdict:block` on the non-zero `errlevel` row). |
| `workflow submit` | **live (write)** | PASS — dry-run `preview`+`confirm_token`; confirmed real submit → `/autoreview/` 302 redirect, workflow id parsed from `Location`; new workflow visible via `list` at `workflow_manreviewing`. Medium-tier (no `--dangerous`). |
| `workflow audit` (single) | **live (reversible)** | PASS — dry-run preview; confirmed `--action pass` → `/passed/`; status advanced to `workflow_review_pass`. `--action cancel` correctly routes to the Cancel view. |
| `workflow audit --ids` (batch) | **live (reversible)** | PASS — shared single `confirm_token`, `items[]`+`summary{total,succeeded,failed,skipped}`; two workflows passed, `succeeded:2`. |
| `workflow auto-review --execute` | **live (reversible)** | PASS — classified compliant, then approved the listed `--ids` via the batch audit path (shared confirm token, `succeeded:1`). Without `--execute` it stays a read-only classify. |
| `workflow execute` | **live drive + contract (high-risk)** | PARTIAL — `--dangerous` double-gate verified (dry-run without it → `E_CONFIRMATION_REQUIRED`); dry-run `preview`+`confirm_token`; confirmed execute reaches the real engine (`/execute/`, status transitions). Terminal status is `workflow_exception` because GoInception's own backup requires MySQL binlog, which is **off** in this `mysql:5.7` e2e container (`binlog日志未开启,无法备份`) — an environment constraint, not a CLI defect. No DDL was applied (engine failed at the CHECKED stage). CLI contract fully verified. |
| `workflow cancel` | **live (reversible)** | PASS — dry-run preview; confirmed `/cancel/` with `cancel_remark`; status → `workflow_abort`. |
| `workflow list --mode jwt` | **contract** | PASS — REST path returns a clean `E_FORBIDDEN` (session auth is not a Bearer token); JWT is opt-in and out of scope for the session run. |

### Defects found and fixed by this run

None. Every `workflow` session-mode endpoint and payload matched v1.8.5 as
shipped; no `archery-cli` code change was needed for the workflow group.

### Environment provisioning (not CLI changes)

The fresh e2e container had no workflow-capable data, so the following Archery
config was provisioned in the isolated container to exercise the write path
(reversible, no CLI code involved). The blockers were surfaced cleanly by the
CLI before each fix:

1. **GoInception unconfigured** (`connect() argument 1 must be str, not None`) —
   set `go_inception_host=goinception`, `go_inception_port=4000` (the reachable
   `tools-e2e-goinception` container).
2. **Instance had no tags** (`你所在组未关联该实例`) — added the `can_write` /
   `can_read` tags to `e2e-mysql`, which the submit RBAC check requires.
3. **No audit flow** (`Column 'audit_auth_groups' cannot be null`) — created a
   `WorkflowAuditSetting` for the resource group (workflow_type 2 = sqlreview)
   and an auth group `cli_verify_auditors` containing the verify user.

All verification workflows (and their audit/log records) were deleted from the
container afterward; no `cli_verify*` tables were created on the target DB
(execute never reached the apply stage). The enabling config above was left in
place so the shared e2e stack stays workflow-capable.

## 2026-06-15 — `binlog` group (session AJAX, v1.8.5)

Live verification of the `binlog list` / `parse` / `purge` commands against the
production-parity `hhyo/archery:v1.8.5` stack on `localhost:9123`, authenticated
with the `cli_verify` superuser in **session mode** (Django form login; the
binlog views have no REST/JWT equivalent). Each command was run with
`--compact`; envelope `ok`/`error` and the documented field shapes were
asserted. No hostnames, credentials, or instance internals are reproduced here.

The stack's `mysql:5.7` had binary logging disabled, so it was enabled for this
run (`server-id`, `log_bin`, `binlog_format=ROW` via `/etc/mysql/conf.d/`) and a
throwaway `binlog_verify` schema with a few DML rows was created to give the
parser real events. The Archery `my2sql` plugin path (a runtime SysConfig key)
was pointed at the bundled `/opt/archery/src/plugins/my2sql` binary. All of this
is local container/stack config — no CLI code is involved.

### Result by command

| Command | Mode | Result |
|---|---|---|
| `binlog list` | **live (read)** | PASS — `show binary logs` returned the live binlog file(s) with `Log_name`/`File_size`; JSON envelope and the table renderer (headers derived from the first row) both verified |
| `binlog parse` | **live (write/medium)** | PASS — dry-run preview + `confirm_token`, then confirm returned **8 real parsed statements** (INSERT/UPDATE/DELETE) for the bounded range; `--rollback` returned the correct inverse SQL. No `--dangerous` needed (medium tier). Required three CLI fixes first (below) |
| `binlog purge` | **live (write/high)** | PASS — `--dangerous` gate enforced in dry-run (omit → `E_CONFIRMATION_REQUIRED`); real purge to a flushed binlog deleted the older files (`purged:true`, confirmed via `show binary logs`); purge to a non-existent file surfaced the server's `status:2` message as `E_VALIDATION`; replaying a spent `confirm_token` → `E_CONFLICT` (single-use) |

### Defects found and fixed by this run

1. **`parse` 500'd when optional numeric fields were omitted.** The v1.8.5
   `my2sql` view parses `num`/`threads`/`start_pos`/`end_pos` as
   `int(value) unless value == ''`; an absent field is `None`, so `int(None)`
   raised and the view returned HTTP 500. The web UI always submits these fields
   (empty when blank). Fixed: `Parse` now always sends
   `start_file/end_file/start_time/stop_time` and (via `emptyIfZero`)
   `start_pos/end_pos/num/threads`, empty when unset, so the view applies its own
   defaults (num=30, threads=4) instead of crashing.
2. **`parse` returned zero rows because sql-types were uppercase.** The bundled
   `my2sql` binary rejects anything but lowercase `insert,update,delete`
   (`invalid sqltypes`), and the view forwards the values verbatim. Fixed:
   `Parse` lowercases each `sql_type[]` before sending. With this fix the same
   request that returned 0 rows now returns the 8 expected statements.
3. **`parse` crashed decoding the view's error envelope.** On arg-check failure
   the view returns `{"status":1,"data":{}}` — `data` is an object, not the
   success path's list — which failed to unmarshal into `[]BinlogParseRow` and
   surfaced as a bogus `E_NETWORK`. Fixed: `data` is now `json.RawMessage` and a
   `Rows()` helper decodes the list only on success, so error envelopes surface
   the server `msg` instead of a parse error.

Unit tests added for all three: empty-when-unset field encoding, lowercase
`sql_type[]`, and the `data:{}` error envelope. `go test ./...`,
`golangci-lint run`, the FCC guard, and the reference guard all stay green.

The throwaway `binlog_verify` schema and the test binlog files were left to the
stack's `expire_logs_days=1`; no `cli_verify*` artifacts were created on the
target DB.

## Reproduce

```bash
export ARCHERY_CLI_URL=http://localhost:9123
export ARCHERY_CLI_USERNAME=admin ARCHERY_CLI_PASSWORD=...
archery-cli doctor --compact
archery-cli instance list --compact
archery-cli dict tables --instance 1 --db archery --compact
archery-cli diagnostic process --instance 1 --compact   # POST + CSRF
```
