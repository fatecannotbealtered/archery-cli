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

## 2026-06-15 — `instance` group, session (AJAX) path

Live run of every `instance` leaf command against the running
`tools-e2e-archery` (`hhyo/archery:v1.8.5`, the production version) on
`localhost:9123`, default **session** mode, superuser `cli_verify`. Reads
verified end-to-end; session-native writes (`create-db`, `create-user`,
`grant`) really submitted against `e2e-mysql` (instance #1) and cleaned up;
REST-only writes verified through dry-run + contract.

### Result by command

| Command | Transport | Status | Note |
|---|---|---|---|
| `instance list` | `POST /instance/list/` | **live PASS** | returns `e2e-mysql`; `--db-type`, `--search`, text table all OK |
| `instance detail <id>` | `POST /instance/list/` (scan) | **live PASS** | found #1; #999 → clean `E_NOT_FOUND` 404 |
| `instance resource --type database\|table\|column` | `GET /instance/instance_resource/` | **live PASS** | uses `tb_name`; rows normalized to `{name:…}` |
| `instance describe` | `POST /instance/describetable/` | **live PASS** | v1.8.5 returns `SHOW CREATE TABLE` (column_list + rows zipped); `Create Table`/`Comment` tagged untrusted |
| `instance users [--saved]` | `POST /instance/user/list` (no trailing slash) | **live PASS** | rows at top level, not under `data`; privileges tagged untrusted |
| `instance test-instance` | `POST /check/instance/` | **live PASS** | `reachable:true` |
| `instance create-db` | `POST /instance/database/create/` | **live PASS** | real DB created + dropped; owner required by the view |
| `instance create-user` | `POST /instance/user/create/` | **live PASS** | real account created (`password1==password2`) + dropped |
| `instance grant` (grant/revoke, global/db) | `POST /instance/user/grant/` | **live PASS** | executed GRANT/REVOKE SQL returned + tagged untrusted |
| `instance create` | `POST /api/v1/instance/` | **dry-run PASS; real = REST-only** | session has no AJAX CRUD route; real submit needs `--mode jwt` |
| `instance update <id>` | `PUT /api/v1/instance/<id>/` | **dry-run PASS; real = REST-only** | same as create |
| `instance delete <id>` | `DELETE /api/v1/instance/<id>/` | **dry-run PASS; real = REST-only** | same as create |
| `instance import --file` | `POST /api/v1/instance/` per row | **dry-run PASS; real = REST-only** | CSV/JSON manifest preview correct |

`create`/`update`/`delete`/`import` are REST-only by design: v1.8.5's
`views.instance` only renders an HTML page and the UI edits instances via Django
admin (`/admin/sql/instance/…`); the programmatic CRUD route is DRF
`/api/v1/instance/`, which requires JWT. In session mode their dry-run/confirm
contract is correct and the real submit honestly surfaces `E_AUTH` until
`--mode jwt` is used.

### Defects found and fixed by this run

1. **Stale session cookie poisoned lazy re-login.** When `ensureSession` fell
   through to a fresh form-login with an already-authenticated `sessionid` in
   the jar, Archery's `/authenticate/` took a branch returning a `session_key`
   in `data` with no new `Set-Cookie`, so `hasSessionCookie()` stayed false and
   login spuriously failed with `session login failed: ok`. Fix: clear
   `sessionid`/`csrftoken` from the jar before a fresh form-login so it always
   starts clean (`clearSessionCookies`, `internal/api/client.go`).
2. **`instance grant` emitted invalid SQL.** The grant view splices `user_host`
   straight into `GRANT … TO <x>`, so the friendly `user@host` produced
   `… TO e2e_probe@%;` → MySQL `1064` syntax error near `'%'`. Fix: normalize
   `--user-host` to the backtick-quoted `` `user`@`host` `` form (pass an
   already-quoted value through), with host defaulting to `%` (`quoteUserHost`,
   `cmd/instance.go`; covered by `TestQuoteUserHost`).

### Note

`instance resource --type database` is served behind Archery's `@cache_page`
(Redis `…insRes…` key), so a freshly dropped database lingers in the list until
the cache expires — a server-side cache, not a CLI defect.
## 2026-06-16 — `user` group, v1.8.5 session path

Live-verified the `user` command group against `hhyo/archery:v1.8.5`
(`tools-e2e-archery`, the company production version) on `localhost:9123`,
authenticated as the superuser `cli_verify` via the default session transport
(lazy Django form login; credentials supplied through `ARCHERY_CLI_*` env vars,
`ARCHERY_CLI_NO_KEYRING=1`). Endpoints cross-checked against the container's
`sql/urls.py`, `sql/user.py`, and `sql/resource_group.py`.

| Command | Transport | Status | Notes |
|---|---|---|---|
| `user list` | session `GET /user/list/` | **live** | 2 users; client-side `--search`/`--offset`/`--limit` and `has_more` verified; table + JSON envelopes correct |
| `user resource-groups` | session `POST /group/group/` | **live** | 1 group `smoke-rg`; server-side `--search` match + empty-set verified; `activeUsers`/`bindUsers` are 0 in session projection (documented) |
| `user groups` | session gate → JWT `GET /api/v1/user/group/` | **live** | session mode returns a clean `E_NOT_FOUND` gate (no session JSON route in v1.8.5); JWT path returns 5/6 groups parsed correctly |
| `user resourcegroup-add` | session `POST /group/addrelation/` | **live** | dry-run `confirm_token` → confirm added user 2 to group 1; upstream membership change verified in DB; validation guards (`--type`/`--ids`/bad id/`--group`) all return `E_VALIDATION`; cleaned up after |

**Defects found:** none. The `user` implementation matched v1.8.5 routes and
payload shapes exactly; all four leaf commands passed on the session path
(plus the JWT fallback for `groups`).

**Verification-only deployment toggles (restored after):**
- `api_user_whitelist` temporarily `1,2` (added `cli_verify` id 2) to exercise
  the JWT `user groups` path — the REST API gates non-whitelisted users with
  403 `IsInUserWhitelist`, which is a deployment config, not a CLI defect.
  Restored to `1`.
- `cli_verify` (id 2) temporarily added to resource group `smoke-rg` (id 1) by
  the `resourcegroup-add` confirm run; removed afterward (members back to
  `[admin]`).

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

## 2026-06-15 — `dict` group, session path (v1.8.5)

Real-machine verification of the `dict` command group against
`tools-e2e-archery` (`hhyo/archery:v1.8.5`) as `cli_verify` (superuser), session
mode. Target instance `e2e-mysql` (mysql), db `archery` (45 tables). No secrets
recorded.

### Result by command

| Command | Class | Result |
| --- | --- | --- |
| `dict tables` | live (read) | PASS — `--instance e2e-mysql --db archery --db-type mysql` returns 45 tables, `{ok,data:{instance,db,tables[],count}}` envelope; `tables[].comment` tagged `_untrusted`. |
| `dict table-info` | live (read) | PASS — `--table sql_instance` returns `meta_data`/`desc`/`index`/`create_sql`; text mode renders Metadata/Columns/Indexes tables. |
| `dict export` | live (read) | PASS — `--db archery` streams the table-structure HTML FileResponse (≈254 KB); raw/text print it verbatim, JSON wraps it in `{format:"html",content}`. |
| `dict views` | gate (contract) | PASS — no `/data_dictionary/view_list/` route in v1.8.5; fails fast with `E_NOT_FOUND` + upgrade message (exit 3), never touches upstream. |
| `dict triggers` | gate (contract) | PASS — same gate for `trigger_list`. |
| `dict procedures` | gate (contract) | PASS — same gate for `procedure_list`. |

Gate honesty confirmed against `sql/urls.py` in the container: v1.8.5 ships only
`table_list`, `table_info`, `export` under `data_dictionary/`.

### Defect found and fixed by this run

**`--db-type` was optional but the v1.8.5 view requires it.** `table_list` and
`table_info` resolve the instance via
`Instance.objects.get(instance_name=…, db_type=…)` — `db_type` is part of the
lookup key. The CLI sent `db_type` only when the flag was present, so the common
case (omitting it) returned `{"status":1,"msg":"Instance.DoesNotExist"}` and
surfaced as a confusing `E_VALIDATION`. Fix: `--db-type` is now **required** for
`dict tables` and `dict table-info` (validated before the request), with a guard
test (`TestDictDbTypeRequired`) and updated flag help. `dict export` is
unaffected — its view keys on `user_instances(...).get(instance_name=…)` with no
`db_type`, so the CLI correctly omits it there.

### Environment note (not a CLI change)

`localhost` resolves to IPv6 `::1` on the verifier host, where the GET /login/
csrftoken and the POST /authenticate/ landed on different bindings and login
intermittently returned `用户名或密码错误`. Using `http://127.0.0.1:9123`
(IPv4) made session login deterministic. This is a host DNS artifact, not a CLI
defect; the session login flow itself (GET csrf → POST authenticate → cookie)
matches the verified curl flow byte-for-byte.

## 2026-06-17 — 1.0.9 endpoint audit: instance-list fallback + detail type fixes

Triggered by two real-usage reports (a non-DBA account getting 403 on `instance
list` while the web could list instances, and `workflow detail 42594` failing to
parse). Method: a **static endpoint audit** cross-referencing every endpoint the
CLI calls against the `hhyo/archery:v1.8.5` source inside the container (route,
permission decorator, request fields, response types) — **zero requests to any
real instance**. Dynamic verification was done **only on the local container**;
the production instance was never used.

| Check | Status | Evidence |
|-------|--------|----------|
| `instance list` 403 → user-scoped fallback | live (local container) | PASS — seeded a non-DBA `cliuser` in resource group `pangu_test` granting `pangu_test_redis`; `instance list --db-type redis --search pangu` returned that instance (id 2) via `/group/user_all_instances/` instead of 403, with a stderr note that host/port need DBA permission. |
| `workflow detail` numeric `execute_time` | unit + earlier real read | PASS — `flexString` accepts number or string; unit test `TestDetail_SessionNumericExecuteTime` encodes the real `0.008516` / numeric `sequence` shape. |
| Parse failure non-retryable | code review | The decode error now returns a status-less `APIError` → `E_UNKNOWN` (non-retryable), not `E_NETWORK`. |
| Audit: admin-only endpoints are correct | static | `instance.lists`=`menu_instance_list`, `user.lists`/`resource_group.group`=`@superuser_required`, `data_dictionary.table_list`=`menu_data_dictionary`, workflow gates=`menu_sqlworkflow`/`sql_submit`/`sql_review`/`audit_user`. No ungated user-scoped session alternative for user/group lists, so they stay admin-only by design. |

The self-fulfilling test pattern that hid the 2FA and instance-list defects is
the audit's core lesson: mocks/fixtures were authored from the client's own
assumptions. New tests encode the **real** Archery contract (numeric field forms,
403-then-fallback) so a regression flips them red.

## 2026-06-17 — 1.0.8 real 2FA login round-trip (closes the prior 2FA gap)

The earlier runs could only mock 2FA because the `cli_verify` account had no 2FA
enabled. A dedicated TOTP account was seeded on the same `tools-e2e-archery`
container (`hhyo/archery:v1.8.5`) — user `cli2fa`, `TwoFactorAuthConfig` with a
known base32 secret — so valid codes can be minted with `pyotp` and the real
`/authenticate/` → `/api/v1/user/2fa/verify/` handshake is exercised end to end.

| Check | Status | Evidence |
|-------|--------|----------|
| Correct OTP completes login | live | PASS — `auth login --otp <fresh>` → `ok:true`, "session cookie cached successfully". The OTP is posted to `/api/v1/user/2fa/verify/` with the temp `session_key` replayed as the `sessionid` cookie. |
| Cached 2FA session is usable | live | PASS — a following `doctor --region local` (no OTP) shows `authValid:true`, `username:cli2fa`, `mode:session`. |
| Wrong OTP rejected cleanly | live | PASS — `--otp 000000` → `ok:false`, server message `验证码不正确！` (real `TwoFAVerify` rejection, not the client's generic fallback). |
| Wrong endpoint regression guard | unit | The faithful mock now requires `/api/v1/user/2fa/verify/` **and** the replayed `sessionid` cookie; reverting either half fails `TestEnsureSession_2FAWithOTPSucceeds`. |

This **closes the "honest gap"** recorded for 1.0.5 below: the real
`/api/v1/user/2fa/verify/` round-trip is now live-verified.

## 2026-06-16 — 1.0.5 features (read-only, 2FA, param unification, detail rework)

Verified against the same `tools-e2e-archery` container (`hhyo/archery:v1.8.5`,
base `http://localhost:9123`) with the ordinary superuser account `cli_verify`,
binary built from this working tree (`go build -o /tmp/a.exe ./cmd/archery-cli`),
`ARCHERY_CLI_NO_KEYRING=1`.

### Result by feature

| Feature | Status | Evidence |
|---------|--------|----------|
| `--read-only` reflected in `doctor` | live (read) | PASS — `doctor` JSON shows `readOnly:true` and a `read-only` warn check; `auth` still `pass` (logged in to the real server). |
| `--read-only` reflected in `context` | live (read) | PASS — `context.data.readOnly == true`. |
| `--read-only` refuses a write | live | PASS — `workflow submit ... --read-only --dry-run` → `E_FORBIDDEN`, exit 4, before any network call. |
| `ARCHERY_CLI_READONLY` env | live | PASS — read command (`workflow list`) succeeds under the env var; the env is a pure read-allow path. |
| Param unification: `--instance`/`--group` IDs (session) | live | PASS — `workflow submit --instance 1 --group 1 --dry-run` preview resolves `instanceName:e2e-mysql`, `groupName:smoke-rg`; confirm submitted real workflow id 4. |
| Unknown instance ID | live | PASS — `--instance 999` → `E_NOT_FOUND` (exit 3) with a clear "check 'instance list'" message. |
| `workflow detail` result rework | live | PASS — `detail 4` returns `result[]` (real auto-review rejection "仅支持DML和DDL语句…") + `statusCode:workflow_autoreviewwrong` + numeric `status:3`. |
| `--otp` harmless for non-2FA account | live | PASS — passing `--otp 000000` for `cli_verify` (no 2FA) does not break login; the OTP path only fires when the server signals 2FA. |
| 2FA detection + `--otp` completion | **mock only** | The `cli_verify` account has **no 2FA enabled**, so the 2FA login branch cannot be exercised live. Covered by unit tests `TestEnsureSession_2FARequiredNoOTP` / `2FAWithOTPSucceeds` / `2FAWrongOTP` against an httptest server that mimics v1.8.5's `/authenticate/` (status 0 + `data` session_key, no sessionid) and `/api/v1/user/2fa/`. **Honest gap: the real `/api/v1/user/2fa/` round-trip is not live-verified.** |

Test workflow id 4 was cancelled after verification (`workflow cancel 4`) to
leave the container clean.

### Honesty notes

- **Race detector not run on this host:** `go test -race` needs cgo + gcc, which
  are unavailable on the verifier machine. The non-race suite (`go test ./...`)
  is fully green, and `golangci-lint run` reports 0 issues.
- **2FA is mock-verified only** for the reason above.

## 2026-06-20 — 1.0.10 `instance list` REST/JWT pagination (issue #12)

Live verification of the `instance list` pagination fix against the running
`tools-e2e-archery` (`hhyo/archery:v1.8.5`) on `localhost:9123`, **`--mode jwt`**,
superuser `cli_verify` (added to the `api_user_whitelist` SysConfig). The fleet
was seeded to **8 instances** so it spans more than one REST page (upstream
`PAGE_SIZE` 5 → 2 pages), then cleaned back down afterward.

### Result by scenario

| Scenario | Command | Status | Result |
|---|---|---|---|
| Full enumeration | `instance list --limit 20` | **live PASS** | all 8 returned (`count:8, total:8, has_more:false`); pre-fix returned only 5 |
| Client-side window | `instance list --limit 3 --offset 2` | **live PASS** | ids 3,4,5; `total:8`; `has_more:true` |
| Cross-page search | `instance list --search page-test` | **live PASS** | 6 matches (ids 3–8 span both pages); `total:6` |
| Exact search | `instance list --search page-test-mysql-5` | **live PASS** | exactly id 7; `total:1` |
| Server-side filter | `instance list --db-type redis` | **live PASS** | id 2 only; `--db-type` still filtered upstream |

Raw `GET /api/v1/instance/?page=1` confirmed the contract: `PageNumberPagination`
envelope `{count:8, next:"…?page=2", previous:null, results:[…]}` with `id` as a
bare JSON number (coerced to string by `instanceResult.UnmarshalJSON`).

### Honesty notes

- **Race detector not run on this host:** `go test -race` needs cgo + gcc, which
  are unavailable on the verifier machine. The non-race suite (`go test ./...`)
  is fully green, `go vet ./...` is clean, and `golangci-lint run` reports 0
  issues. CI runs `-race` on Linux.
- The new mock-contract test (`TestListInstancesREST_PageNumberPagination`)
  encodes the real `PageNumberPagination` semantics observed above (page walk,
  null `next` terminator, numeric `id`, no server-side search/limit/offset).

## 2026-06-21 — 1.0.11 `instance resource` REST/JWT parsing (issue #13)

Live verification of the `instance resource` REST fix against the running
`tools-e2e-archery` (`hhyo/archery:v1.8.5`) on `localhost:9123`, **`--mode jwt`**,
superuser `cli_verify`.

The raw `POST /api/v1/instance/resource/` envelope was captured first:
`{"count":1,"result":["archery"]}` (database), `{"count":46,"result":[…]}`
(table), `{"count":5,"result":["id","username",…]}` (column) — singular `result`,
flat scalar names. Pre-fix the CLI parsed only the plural `results`, so all three
returned `data: null`.

### Result by scenario

| Scenario | Command | Status | Result |
|---|---|---|---|
| Databases | `instance resource --instance 1 --type database` | **live PASS** | `[{"name":"archery"}]` (was `data:null`) |
| Tables | `instance resource --instance 1 --type table --db archery` | **live PASS** | 46 tables as `{"name":…}` |
| Columns | `instance resource --instance 1 --type column --db archery --table 2fa_config` | **live PASS** | `id, username, auth_type, secret_key, user_id` |

### Confirmed server limitation (documented, not fixed)

- `query run` (`/query/`) and `instance describe` (`/instance/describetable/`)
  have no REST/JWT route in Archery's `sql_api/urls.py` (the `/api/v1` surface is
  user/instance/workflow only). They stay session-only and return `E_AUTH` under
  `--mode jwt`; this is now documented in the `query`/`instance` Skill references.

### Honesty notes

- **Race detector not run on this host:** `go test -race` needs cgo + gcc,
  unavailable here. Non-race suite green, `go vet` clean, `golangci-lint` 0
  issues; CI runs `-race` on Linux.
- New mock-contract test `TestResourceREST_CountResultEnvelope` encodes the real
  `{count, result}` envelope (singular `result`, scalar rows, POST verb/path).

## Reproduce

```bash
export ARCHERY_CLI_URL=http://localhost:9123
export ARCHERY_CLI_USERNAME=admin ARCHERY_CLI_PASSWORD=...
archery-cli doctor --compact
archery-cli instance list --compact
archery-cli dict tables --instance 1 --db archery --compact
archery-cli diagnostic process --instance 1 --compact   # POST + CSRF
```
