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

## Reproduce

```bash
export ARCHERY_CLI_URL=http://localhost:9123
export ARCHERY_CLI_USERNAME=admin ARCHERY_CLI_PASSWORD=...
archery-cli doctor --compact
archery-cli instance list --compact
archery-cli dict tables --instance 1 --db archery --compact
archery-cli diagnostic process --instance 1 --compact   # POST + CSRF
```
