# Instances

Manage database instances: list, view details, browse resources (databases, schemas, tables, columns), describe table structure, create/update/delete instances, find instances by table name, and list database users.

## Table of Contents

- [Read commands](#read-commands)
- [Write commands](#write-commands)
- [Instance data payload](#instance-data-payload)
- [Workflows](#workflows)
- [Notes](#notes)

## Read commands

### List instances

```bash
archery-cli instance list --compact
archery-cli instance list --db-type mysql --compact
archery-cli instance list --type master --search "prod" --compact
archery-cli instance list --fields id,instanceName,dbType,host --compact
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--type` | string | (all) | Filter by type: `master` or `slave` |
| `--db-type` | string | (all) | Filter by database type: `mysql`, `pgsql`, `mssql`, `redis`, etc. |
| `--search` | string | -- | Search by instance name |
| `--limit` | int | 20 | Max results per page (1-500) |
| `--offset` | int | 0 | Pagination offset |
| `--fields` | string | -- | Comma-separated output fields |

### Instance detail

```bash
archery-cli instance detail 42
archery-cli instance detail 42 --fields id,instanceName,dbType,host,port
```

### Browse resources (databases, schemas, tables, columns)

```bash
# List databases on an instance
archery-cli instance resource --instance 1 --type database

# List schemas
archery-cli instance resource --instance 1 --type schema --db mydb

# List tables in a database
archery-cli instance resource --instance 1 --type table --db mydb

# List columns in a table
archery-cli instance resource --instance 1 --type column --db mydb --table users
```

| Flag | Type | Required | Description |
|------|------|----------|-------------|
| `--instance` | int | yes | Instance ID |
| `--type` | string | yes | Resource type: `database`, `schema`, `table`, `column` |
| `--db` | string | no | Database name (required for schema/table/column) |
| `--schema` | string | no | Schema name (for column listing) |
| `--table` | string | no | Table name (required for column listing) |
| `--fields` | string | no | Comma-separated output fields |

### Describe table structure

```bash
archery-cli instance describe --instance prod-mysql --db mydb --table users
archery-cli instance describe --instance prod-mysql --db mydb --table users --schema public
```

| Flag | Type | Required | Description |
|------|------|----------|-------------|
| `--instance` | string | yes | Instance name (not ID) |
| `--db` | string | yes | Database name |
| `--table` | string | yes | Table name |
| `--schema` | string | no | Schema name |

### Find instances containing a table

```bash
archery-cli instance table-instances --table users
```

| Flag | Type | Required | Description |
|------|------|----------|-------------|
| `--table` | string | yes | Table name to search for |

### List database users

```bash
archery-cli instance users --instance 1
archery-cli instance users --instance 1 --saved
```

| Flag | Type | Required | Description |
|------|------|----------|-------------|
| `--instance` | int | yes | Instance ID |
| `--saved` | bool | no | Filter by saved users only |

## Write commands

All write commands require `--dry-run` first, then `--confirm <token>`.

### Create an instance

```bash
archery-cli instance create \
  --name "prod-mysql-replica" \
  --type slave \
  --db-type mysql \
  --host 10.0.1.50 \
  --port 3306 \
  --user readonly \
  --password "secret" \
  --mode cluster \
  --charset utf8mb4 \
  --dry-run

archery-cli instance create ... --confirm ct_...
```

| Flag | Type | Required | Description |
|------|------|----------|-------------|
| `--name` | string | yes | Instance name |
| `--type` | string | yes | `master` or `slave` |
| `--db-type` | string | yes | Database type: `mysql`, `pgsql`, `mssql`, `redis`, etc. |
| `--host` | string | yes | Host address |
| `--port` | int | yes | Port number (1-65535) |
| `--user` | string | yes | Database user |
| `--password` | string | no | Database password |
| `--mode` | string | no | `standalone` or `cluster` |
| `--db` | string | no | Default database name |
| `--charset` | string | no | Character set |

### Update an instance

```bash
archery-cli instance update 42 --host 10.0.1.51 --port 3307 --dry-run
archery-cli instance update 42 --host 10.0.1.51 --port 3307 --confirm ct_...
```

At least one field to update is required. Passwords are redacted in dry-run preview.

### Delete an instance

```bash
archery-cli instance delete 42 --dry-run
archery-cli instance delete 42 --confirm ct_...
```

- Risk level: **high** -- always confirm with user before deleting

## Instance data payload

```json
{
  "id": 42,
  "instanceName": "prod-mysql",
  "dbType": "mysql",
  "host": "10.0.1.10",
  "port": 3306,
  "user": "admin",
  "dbName": "mydb",
  "charset": "utf8mb4",
  "environment": "production",
  "isActive": true,
  "instanceTag": "critical"
}
```

### List response shape

```json
{"items":[...],"count":20,"limit":20,"total":50,"has_more":true}
```

## Workflows

### Explore a new instance

```bash
archery-cli instance list --search "staging" --compact
archery-cli instance detail 42
archery-cli instance resource --instance 42 --type database
archery-cli instance resource --instance 42 --type table --db myapp
archery-cli instance describe --instance staging-mysql --db myapp --table orders
```

### Find where a table lives

```bash
archery-cli instance table-instances --table orders
archery-cli instance detail 7
archery-cli instance describe --instance prod-mysql --db mydb --table orders
```

## Notes

- `instance detail` takes a positional INSTANCE_ID argument: `archery-cli instance detail 42`
- `instance describe` uses `--instance` flag with the **instance name** (not ID), while `instance resource` uses `--instance` flag with the **instance ID** (integer)
- `instance delete` risk level is **high** -- irreversible, always confirm with user
- `instance create` risk level is **high** -- confirm parameters with user before creating
- `instance resource` types: `database`, `schema`, `table`, `column` (hierarchical: each level requires the parent)
- `instance users` lists database-level users, not Archery platform users
- `instance table-instances` searches across all registered instances for a given table name
- All IDs are integers; all instance names are strings
