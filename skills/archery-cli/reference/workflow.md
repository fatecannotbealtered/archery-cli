# SQL Workflows

Manage SQL audit workflows: submit SQL for review, audit (approve/reject), execute approved workflows, cancel, and run sqlcheck.

## Table of Contents

- [Read commands](#read-commands)
- [Write commands](#write-commands)
- [Workflow data payload](#workflow-data-payload)
- [Workflows](#workflows)
- [Notes](#notes)

## Read commands

```bash
# List workflows with optional filters
archery-cli workflow list --compact
archery-cli workflow list --status workflow_finish --compact
archery-cli workflow list --engineer admin --compact
archery-cli workflow list --instance 1 --db mydb --limit 50 --compact
archery-cli workflow list --fields id,name,status,engineer --compact

# Get workflow details (SQL content, audit log, execution status)
archery-cli workflow detail 42
archery-cli workflow detail 42 --fields id,name,status,sql

# Run SQL syntax and risk check (no side effects, no workflow created)
archery-cli workflow sqlcheck --instance 1 --db mydb --sql "ALTER TABLE users ADD INDEX idx_email (email)"
```

### `workflow list` flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--status` | string | (all) | Filter by status (e.g. `workflow_finish`, `audit_abort`, `workflow_manconfirming`) |
| `--engineer` | string | (all) | Filter by creator username |
| `--instance` | int | (all) | Filter by instance ID |
| `--db` | string | (all) | Filter by database name |
| `--limit` | int | 20 | Max results per page (1-500) |
| `--offset` | int | 0 | Pagination offset |
| `--fields` | string | (all) | Comma-separated output fields |

### `workflow detail` flags

| Flag | Type | Description |
|------|------|-------------|
| `--fields` | string | Comma-separated output fields |

### `workflow sqlcheck` flags

| Flag | Type | Required | Description |
|------|------|----------|-------------|
| `--instance` | int | yes | Target instance ID |
| `--db` | string | yes | Target database name |
| `--sql` | string | yes | SQL to check |

## Write commands

All write commands require `--dry-run` first, then `--confirm <token>`.

### Submit a workflow

```bash
archery-cli workflow submit --name "Add email index" --instance 1 --db mydb \
  --sql "ALTER TABLE users ADD INDEX idx_email (email)" --dry-run

archery-cli workflow submit --name "Add email index" --instance 1 --db mydb \
  --sql "ALTER TABLE users ADD INDEX idx_email (email)" --confirm ct_...
```

| Flag | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `--name` | string | yes | -- | Workflow title |
| `--instance` | int | yes | -- | Target instance ID |
| `--db` | string | yes | -- | Target database name |
| `--sql` | string | yes | -- | SQL content |
| `--group` | int | no | (auto) | Resource group ID |
| `--backup` | bool | no | true | Require backup before execution |
| `--demand-url` | string | no | -- | Related demand/requirement URL |

### Audit (approve or reject) a workflow

```bash
archery-cli workflow audit 42 --action pass --remark "LGTM" --dry-run
archery-cli workflow audit 42 --action pass --remark "LGTM" --confirm ct_...
```

| Flag | Type | Required | Description |
|------|------|----------|-------------|
| `--action` | string | yes | `pass` or `cancel` |
| `--remark` | string | no | Audit remark/comment |

### Execute an approved workflow

```bash
archery-cli workflow execute 42 --mode auto --dangerous --dry-run
archery-cli workflow execute 42 --mode auto --dangerous --confirm ct_...
```

| Flag | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `--mode` | string | no | auto | `auto` or `manual` execution mode |

### Cancel a running workflow

```bash
archery-cli workflow cancel 42 --remark "No longer needed" --dry-run
archery-cli workflow cancel 42 --remark "No longer needed" --confirm ct_...
```

| Flag | Type | Description |
|------|------|-------------|
| `--remark` | string | Cancellation remark |

## Workflow data payload

```json
{
  "id": "42",
  "name": "Add email index",
  "status": "workflow_finish",
  "engineer": "admin",
  "instance": "1",
  "db_name": "mydb",
  "sql_content": "ALTER TABLE users ADD INDEX idx_email (email)",
  "create_time": "2024-06-15T10:30:00Z",
  "url": "https://archery.example.com/sqlworkflow/42/"
}
```

### List response shape

```json
{
  "items": [...],
  "count": 20,
  "limit": 20,
  "total": 150,
  "has_more": true
}
```

## Workflows

### Submit SQL for audit then execute after approval

```bash
# 1. Optionally run sqlcheck first
archery-cli workflow sqlcheck --instance 1 --db mydb --sql "ALTER TABLE orders ADD COLUMN note VARCHAR(255)"

# 2. Submit
archery-cli workflow submit --name "Add note column" --instance 1 --db mydb \
  --sql "ALTER TABLE orders ADD COLUMN note VARCHAR(255)" --dry-run
archery-cli workflow submit --name "Add note column" --instance 1 --db mydb \
  --sql "ALTER TABLE orders ADD COLUMN note VARCHAR(255)" --confirm ct_...

# 3. Check status
archery-cli workflow detail 42

# 4. Execute after approval
archery-cli workflow execute 42 --mode auto --dangerous --dry-run
archery-cli workflow execute 42 --mode auto --dangerous --confirm ct_...
```

## Notes

- `workflow list` supports `--fields` for output trimming in JSON mode
- `workflow submit` returns `workflowId` and `url` in the response
- `workflow execute` risk level is **high** -- requires `--dangerous` in both dry-run and confirm steps
- `workflow audit` action must be exactly `pass` or `cancel`
- Workflow status values: `workflow_manconfirming`, `workflow_finish`, `audit_abort`, `workflow_executing`, etc.
- All write operations are audit-logged to `~/.archery-cli/audit/`
