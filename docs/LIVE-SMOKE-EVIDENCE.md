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
