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
| `workflow execute --ids` | **dry-run only** | PASS (dry-run) — irreversible; per safety policy not run for real. Preview/`confirm_token`/`dangerous:true` verified. NOT executed against real workflows |
| `instance import --file` | **dry-run only** | PASS (dry-run) — write/irreversible; dry-run only. CSV/JSON manifest parsed, per-row `create` preview emitted, **password field not echoed in the preview envelope**. NOT imported for real |

### Cross-cutting contract points (live-verified on `query run --instances`)

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
