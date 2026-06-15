package cmd

// This file is the single source of truth for the machine-readable
// `output_schema` label and runnable `examples` carried by every leaf command in
// `archery-cli reference`. Keeping the field enumeration here (rather than a
// generic {envelope,object} stub) lets an agent know the exact JSON `data` shape
// — including which keys are `_untrusted` external data — before it ever runs a
// command.
//
// Two pieces stay in lock-step, guarded by reference_schema_test.go:
//   - referenceLeafSchemas: leaf command path -> {schema label, examples}.
//   - referenceSchemas():   schema label -> field/untrusted enumeration.
//
// Field lists mirror the *ToMap builders in internal/output/flatten.go, the
// inline maps each command emits, and the auto-tagging in agent_output.go
// (tagCommonUntrusted) plus explicit api.TagUntrusted calls. List shapes use the
// shared envelopes: standard NewListEnvelope (items+count+limit+page+total+
// has_more+all) and the offset cursor offsetListEnvelope (items+total+count+
// offset+has_more+next_offset) used by archive/slowquery/query log.

// referenceLeaf pairs a command's output schema label with one runnable example.
type referenceLeaf struct {
	schema   string
	examples []string
}

// referenceLeafSchemas maps each leaf command path to its real output schema and
// a runnable example. Write commands show the dry-run then the confirm step;
// high/critical-risk writes (the --dangerous gate) carry --dangerous in both.
var referenceLeafSchemas = map[string]referenceLeaf{
	// ─── workflow ─────────────────────────────────────────────────────────────
	"workflow list":        {schema: "workflow_list", examples: []string{"archery-cli workflow list --status workflow_finish --limit 20 --compact"}},
	"workflow audit-list":  {schema: "workflow_list", examples: []string{"archery-cli workflow audit-list --limit 20 --compact"}},
	"workflow submit":      {schema: "workflow_submit", examples: []string{"archery-cli workflow submit --name 'add idx' --instance-name prod-mysql --group-name dev --db app --sql 'ALTER TABLE t ADD INDEX(x)' --dry-run --compact", "archery-cli workflow submit --name 'add idx' --instance-name prod-mysql --group-name dev --db app --sql 'ALTER TABLE t ADD INDEX(x)' --confirm <confirm_token> --compact"}},
	"workflow detail":      {schema: "workflow_detail", examples: []string{"archery-cli workflow detail 42 --compact"}},
	"workflow audit":       {schema: "workflow_audit", examples: []string{"archery-cli workflow audit 42 --action pass --dry-run --compact", "archery-cli workflow audit 42 --action pass --confirm <confirm_token> --compact", "archery-cli workflow audit --ids 42,43,44 --action pass --dry-run --compact", "archery-cli workflow audit --ids 42,43,44 --action pass --confirm <confirm_token> --compact"}},
	"workflow execute":     {schema: "workflow_execute", examples: []string{"archery-cli workflow execute 42 --dangerous --dry-run --compact", "archery-cli workflow execute 42 --dangerous --confirm <confirm_token> --compact", "archery-cli workflow execute --ids 42,43 --dangerous --dry-run --compact", "archery-cli workflow execute --ids 42,43 --dangerous --confirm <confirm_token> --compact"}},
	"workflow cancel":      {schema: "workflow_cancel", examples: []string{"archery-cli workflow cancel 42 --remark 'abort' --dry-run --compact", "archery-cli workflow cancel 42 --remark 'abort' --confirm <confirm_token> --compact"}},
	"workflow sqlcheck":    {schema: "sqlcheck_result", examples: []string{"archery-cli workflow sqlcheck --instance-name prod-mysql --db app --sql 'SELECT 1' --compact"}},
	"workflow auto-review": {schema: "workflow_auto_review", examples: []string{"archery-cli workflow auto-review --instance-name prod-mysql --db app --sql 'UPDATE t SET x=1 WHERE id=1' --compact", "archery-cli workflow auto-review --instance-name prod-mysql --db app --sql 'UPDATE t SET x=1 WHERE id=1' --execute --ids 42,43 --dry-run --compact", "archery-cli workflow auto-review --instance-name prod-mysql --db app --sql 'UPDATE t SET x=1 WHERE id=1' --execute --ids 42,43 --confirm <confirm_token> --compact"}},

	// ─── instance ─────────────────────────────────────────────────────────────
	"instance list":          {schema: "instance_list", examples: []string{"archery-cli instance list --db-type mysql --limit 20 --compact"}},
	"instance detail":        {schema: "instance_detail", examples: []string{"archery-cli instance detail 1 --compact"}},
	"instance resource":      {schema: "resource_item_list", examples: []string{"archery-cli instance resource --instance 1 --type database --compact"}},
	"instance describe":      {schema: "instance_describe", examples: []string{"archery-cli instance describe --instance prod-mysql --db app --table users --compact"}},
	"instance create":        {schema: "instance_detail", examples: []string{"archery-cli instance create --name prod-mysql --type master --db-type mysql --host db.example.com --port 3306 --user app --dangerous --dry-run --compact", "archery-cli instance create --name prod-mysql --type master --db-type mysql --host db.example.com --port 3306 --user app --dangerous --confirm <confirm_token> --compact"}},
	"instance import":        {schema: "batch_result", examples: []string{"archery-cli instance import --file instances.csv --dangerous --dry-run --compact", "archery-cli instance import --file instances.csv --dangerous --confirm <confirm_token> --compact"}},
	"instance update":        {schema: "instance_detail", examples: []string{"archery-cli instance update 1 --host db2.example.com --dangerous --dry-run --compact", "archery-cli instance update 1 --host db2.example.com --dangerous --confirm <confirm_token> --compact"}},
	"instance delete":        {schema: "instance_delete", examples: []string{"archery-cli instance delete 1 --dangerous --dry-run --compact", "archery-cli instance delete 1 --dangerous --confirm <confirm_token> --compact"}},
	"instance users":         {schema: "instance_user_list", examples: []string{"archery-cli instance users --instance 1 --compact"}},
	"instance test-instance": {schema: "instance_test", examples: []string{"archery-cli instance test-instance --instance 1 --compact"}},
	"instance create-db":     {schema: "instance_create_db", examples: []string{"archery-cli instance create-db --instance 1 --db reporting --owner alice --dangerous --dry-run --compact", "archery-cli instance create-db --instance 1 --db reporting --owner alice --dangerous --confirm <confirm_token> --compact"}},
	"instance create-user":   {schema: "instance_create_user", examples: []string{"archery-cli instance create-user --instance 1 --user app --host '%' --password 'S3cret!' --dangerous --dry-run --compact", "archery-cli instance create-user --instance 1 --user app --host '%' --password 'S3cret!' --dangerous --confirm <confirm_token> --compact"}},
	"instance grant":         {schema: "instance_grant", examples: []string{"archery-cli instance grant --instance 1 --user-host '`app`@`%`' --op grant --level db --db reporting --privs SELECT,INSERT --dangerous --dry-run --compact", "archery-cli instance grant --instance 1 --user-host '`app`@`%`' --op grant --level db --db reporting --privs SELECT,INSERT --dangerous --confirm <confirm_token> --compact"}},

	// ─── query ────────────────────────────────────────────────────────────────
	"query run":      {schema: "query_run", examples: []string{"archery-cli query run --instance prod-mysql --db app --sql 'SELECT * FROM users LIMIT 10' --dangerous --dry-run --compact", "archery-cli query run --instance prod-mysql --db app --sql 'SELECT * FROM users LIMIT 10' --dangerous --confirm <confirm_token> --compact", "archery-cli query run --instances prod-mysql,prod-mysql-2 --db app --sql 'SELECT COUNT(*) FROM users' --dangerous --dry-run --compact", "archery-cli query run --instances prod-mysql,prod-mysql-2 --db app --sql 'SELECT COUNT(*) FROM users' --dangerous --confirm <confirm_token> --compact"}},
	"query explain":  {schema: "query_explain", examples: []string{"archery-cli query explain --instance prod-mysql --db app --sql 'SELECT * FROM users' --compact"}},
	"query log":      {schema: "query_log_list", examples: []string{"archery-cli query log --limit 20 --compact"}},
	"query favorite": {schema: "query_favorite", examples: []string{"archery-cli query favorite 123 --star --dry-run --compact", "archery-cli query favorite 123 --star --confirm <confirm_token> --compact"}},
	"query generate": {schema: "query_generate", examples: []string{"archery-cli query generate --instance prod-mysql --db app --table users --desc 'active users in last 7 days' --compact"}},

	// ─── archive ──────────────────────────────────────────────────────────────
	"archive list":   {schema: "archive_list", examples: []string{"archery-cli archive list --state enable --limit 20 --compact"}},
	"archive apply":  {schema: "archive_raw", examples: []string{"archery-cli archive apply --title 'archive logs' --group ops --src-instance prod --src-db app --src-table logs --mode purge --condition 'create_time < \"2024-01-01\"' --dangerous --dry-run --compact", "archery-cli archive apply --title 'archive logs' --group ops --src-instance prod --src-db app --src-table logs --mode purge --condition 'create_time < \"2024-01-01\"' --dangerous --confirm <confirm_token> --compact"}},
	"archive audit":  {schema: "archive_raw", examples: []string{"archery-cli archive audit 7 --action pass --dry-run --compact", "archery-cli archive audit 7 --action pass --confirm <confirm_token> --compact"}},
	"archive switch": {schema: "archive_raw", examples: []string{"archery-cli archive switch 7 --state enable --dry-run --compact", "archery-cli archive switch 7 --state enable --confirm <confirm_token> --compact"}},
	"archive once":   {schema: "archive_raw", examples: []string{"archery-cli archive once 7 --dangerous --dry-run --compact", "archery-cli archive once 7 --dangerous --confirm <confirm_token> --compact"}},
	"archive log":    {schema: "archive_log_list", examples: []string{"archery-cli archive log 7 --limit 20 --compact"}},

	// ─── slowquery ────────────────────────────────────────────────────────────
	"slowquery review":   {schema: "slowquery_list", examples: []string{"archery-cli slowquery review --instance prod-mysql --start '2026-06-01 00:00:00' --end '2026-06-07 23:59:59' --limit 20 --compact"}},
	"slowquery history":  {schema: "slowquery_history", examples: []string{"archery-cli slowquery history --instance prod-mysql --start '2026-06-01 00:00:00' --end '2026-06-07 23:59:59' --sql-id abc123 --compact"}},
	"slowquery optimize": {schema: "slowquery_optimize", examples: []string{"archery-cli slowquery optimize --instance prod-mysql --db app --sql 'SELECT * FROM users' --tool soar --compact"}},

	// ─── binlog ───────────────────────────────────────────────────────────────
	"binlog list":  {schema: "binlog_list", examples: []string{"archery-cli binlog list --instance prod-mysql --compact"}},
	"binlog parse": {schema: "binlog_parse", examples: []string{"archery-cli binlog parse --instance prod-mysql --start-file mysql-bin.000123 --dry-run --compact", "archery-cli binlog parse --instance prod-mysql --start-file mysql-bin.000123 --confirm <confirm_token> --compact"}},
	"binlog purge": {schema: "binlog_purge", examples: []string{"archery-cli binlog purge --instance 1 --binlog mysql-bin.000100 --dangerous --dry-run --compact", "archery-cli binlog purge --instance 1 --binlog mysql-bin.000100 --dangerous --confirm <confirm_token> --compact"}},

	// ─── diagnostic ───────────────────────────────────────────────────────────
	"diagnostic process":      {schema: "diagnostic_row_list", examples: []string{"archery-cli diagnostic process --instance prod-mysql --compact"}},
	"diagnostic kill":         {schema: "diagnostic_kill", examples: []string{"archery-cli diagnostic kill --instance prod-mysql --threads 101,102 --dangerous --dry-run --compact", "archery-cli diagnostic kill --instance prod-mysql --threads 101,102 --dangerous --confirm <confirm_token> --compact"}},
	"diagnostic tablespace":   {schema: "diagnostic_row_list", examples: []string{"archery-cli diagnostic tablespace --instance prod-mysql --compact"}},
	"diagnostic locks":        {schema: "diagnostic_row_list", examples: []string{"archery-cli diagnostic locks --instance prod-mysql --compact"}},
	"diagnostic transactions": {schema: "diagnostic_row_list", examples: []string{"archery-cli diagnostic transactions --instance prod-mysql --compact"}},

	// ─── dict ─────────────────────────────────────────────────────────────────
	"dict tables":     {schema: "dict_tables", examples: []string{"archery-cli dict tables --instance prod-mysql --db app --db-type mysql --compact"}},
	"dict table-info": {schema: "dict_table_info", examples: []string{"archery-cli dict table-info --instance prod-mysql --db app --table users --db-type mysql --compact"}},
	"dict views":      {schema: "dict_unavailable", examples: []string{"archery-cli dict views --instance prod-mysql --db app --compact"}},
	"dict triggers":   {schema: "dict_unavailable", examples: []string{"archery-cli dict triggers --instance prod-mysql --db app --compact"}},
	"dict procedures": {schema: "dict_unavailable", examples: []string{"archery-cli dict procedures --instance prod-mysql --db app --compact"}},
	"dict export":     {schema: "dict_export", examples: []string{"archery-cli dict export --instance prod-mysql --db app --raw"}},

	// ─── user ─────────────────────────────────────────────────────────────────
	"user list":              {schema: "user_list", examples: []string{"archery-cli user list --limit 20 --compact"}},
	"user groups":            {schema: "group_list", examples: []string{"archery-cli user groups --mode jwt --limit 20 --compact"}},
	"user resource-groups":   {schema: "resource_group_list", examples: []string{"archery-cli user resource-groups --limit 20 --compact"}},
	"user resourcegroup-add": {schema: "resource_group_add", examples: []string{"archery-cli user resourcegroup-add --group 1 --type user --ids 3,4 --dry-run --compact", "archery-cli user resourcegroup-add --group 1 --type user --ids 3,4 --confirm <confirm_token> --compact"}},

	// ─── auth ─────────────────────────────────────────────────────────────────
	"auth login":  {schema: "auth_login", examples: []string{"archery-cli auth login --url https://archery.example.com --username admin --password secret --dry-run --compact", "archery-cli auth login --url https://archery.example.com --username admin --password secret --confirm <confirm_token> --compact"}},
	"auth logout": {schema: "auth_logout", examples: []string{"archery-cli auth logout --dry-run --compact", "archery-cli auth logout --confirm <confirm_token> --compact"}},
	"auth status": {schema: "auth_status", examples: []string{"archery-cli auth status --compact"}},

	// ─── self-description ─────────────────────────────────────────────────────
	"context":   {schema: "context", examples: []string{"archery-cli context --compact"}},
	"doctor":    {schema: "doctor", examples: []string{"archery-cli doctor --compact"}},
	"reference": {schema: "reference", examples: []string{"archery-cli reference --compact"}},
	"changelog": {schema: "changelog", examples: []string{"archery-cli changelog --since 1.0.0 --compact"}},
	"update":    {schema: "update_report", examples: []string{"archery-cli update --check --compact", "archery-cli update --dry-run --compact", "archery-cli update --confirm <confirm_token> --compact"}},
}

// listEnvelopeFields are the standard NewListEnvelope meta keys wrapping `items`.
var listEnvelopeFields = []string{"items", "count", "limit", "page", "total", "has_more", "all"}

// offsetEnvelopeFields are the offset-cursor envelope keys (archive/slowquery/
// query log) wrapping `items`.
var offsetEnvelopeFields = []string{"items", "total", "count", "offset", "has_more", "next_offset"}

// referenceSchemas enumerates every output schema label referenced by
// referenceLeafSchemas. Each entry lists the concrete field keys and the
// `_untrusted` subset (external data, not instructions; SEC-SPEC §2).
func referenceSchemas() map[string]refDataSchema {
	return map[string]refDataSchema{
		// ─── workflow ─────────────────────────────────────────────────────────
		"workflow_list":        {Shape: "list", Fields: append(append([]string{}, listEnvelopeFields...), "id", "title", "status", "statusCode", "engineer", "instanceId", "instance", "db", "group", "created", "webUrl"), UntrustedFields: []string{"title"}},
		"workflow_submit":      {Shape: "object", Fields: []string{"workflowId", "url"}},
		"workflow_detail":      {Shape: "object", Fields: []string{"id", "title", "status", "engineer", "instanceId", "db", "groupId", "sql", "created", "demandUrl", "webUrl", "auditLog"}, UntrustedFields: []string{"title", "sql", "auditLog", "demandUrl"}},
		"workflow_audit":       {Shape: "object", Fields: []string{"workflowId", "status"}},
		"workflow_execute":     {Shape: "object", Fields: []string{"workflowId", "status", "mode"}},
		"workflow_cancel":      {Shape: "object", Fields: []string{"workflowId", "status"}},
		"sqlcheck_result":      {Shape: "array", Fields: []string{"level", "message", "affected_rows", "sql", "errlevel", "stagestatus"}, UntrustedFields: []string{"sql"}},
		"workflow_auto_review": {Shape: "object", Fields: []string{"compliant", "blocked", "results", "results[].sql", "results[].errlevel", "results[].level", "results[].stagestatus", "results[].message", "results[].affectedRows", "results[].verdict", "items", "summary", "preview", "confirm_token", "expires_at"}, UntrustedFields: []string{"results[].sql", "results[].message"}},

		// ─── instance ─────────────────────────────────────────────────────────
		"instance_list":        {Shape: "list", Fields: append(append([]string{}, listEnvelopeFields...), "id", "instanceName", "dbType", "host", "port", "user", "dbName", "charset", "environment", "instanceTag", "isActive")},
		"instance_detail":      {Shape: "object", Fields: []string{"id", "instanceName", "dbType", "host", "port", "user", "dbName", "charset", "environment", "instanceTag", "isActive"}},
		"instance_delete":      {Shape: "object", Fields: []string{"deleted", "instanceId"}},
		"instance_user_list":   {Shape: "array", Fields: []string{"<dynamic upstream user row keys: user_host, user, host, privileges, saved, is_locked, ...>"}, UntrustedFields: []string{"privileges"}},
		"instance_describe":    {Shape: "object", Fields: []string{"instance", "db", "table", "schema", "columns"}, UntrustedFields: []string{"columns[].comment", "columns[].Comment", "columns[].Create Table"}},
		"resource_item_list":   {Shape: "array", Fields: []string{"<dynamic resource row keys: name (database|schema|table) or colN (column)>"}},
		"instance_test":        {Shape: "object", Fields: []string{"instanceId", "reachable", "message"}, UntrustedFields: []string{"message"}},
		"instance_create_db":   {Shape: "object", Fields: []string{"created", "instanceId", "db"}},
		"instance_create_user": {Shape: "object", Fields: []string{"created", "instanceId", "user", "host"}},
		"instance_grant":       {Shape: "object", Fields: []string{"applied", "instanceId", "userHost", "sql"}, UntrustedFields: []string{"sql"}},

		// ─── batch (CLI-SPEC §15) ──────────────────────────────────────────────
		// Shared shape for client-side batch commands (class B): per-item items[]
		// plus a {total,succeeded,failed,skipped} summary. dry-run instead returns
		// {preview{action,total,targets,changes},confirm_token,expires_at}. Each
		// item's data mirrors the single-target schema for that command. _untrusted
		// is carried inside each item's data (e.g. query run rows).
		"batch_result": {Shape: "object", Fields: []string{"items", "items[].target", "items[].ok", "items[].data", "items[].error", "items[].skipped", "summary", "summary.total", "summary.succeeded", "summary.failed", "summary.skipped", "preview", "confirm_token", "expires_at"}, UntrustedFields: []string{"items[].data.rows"}},

		// ─── query ────────────────────────────────────────────────────────────
		"query_run":      {Shape: "object", Fields: []string{"columns", "rows", "row_count", "query_time_ms", "masked"}, UntrustedFields: []string{"rows"}},
		"query_explain":  {Shape: "object", Fields: []string{"plan"}, UntrustedFields: []string{"plan"}},
		"query_log_list": {Shape: "list", Fields: append(append([]string{}, offsetEnvelopeFields...), "id", "username", "db", "sql", "effect_row", "cost_time", "instance", "exec_time", "favorite", "alias"), UntrustedFields: []string{"sql", "alias"}},
		"query_favorite": {Shape: "object", Fields: []string{"id", "star"}},
		"query_generate": {Shape: "object", Fields: []string{"sql"}, UntrustedFields: []string{"sql"}},

		// ─── archive ──────────────────────────────────────────────────────────
		"archive_list":     {Shape: "list", Fields: append(append([]string{}, offsetEnvelopeFields...), "id", "title", "state", "src_instance__instance_name", "src_db_name", "src_table_name", "mode"), UntrustedFields: []string{"title"}},
		"archive_log_list": {Shape: "list", Fields: append(append([]string{}, offsetEnvelopeFields...), "start_time", "end_time", "mode", "success", "select_cnt", "insert_cnt", "delete_cnt")},
		"archive_raw":      {Shape: "object", Fields: []string{"<upstream archive apply/audit/switch response>"}},

		// ─── slowquery ────────────────────────────────────────────────────────
		"slowquery_list":     {Shape: "list", Fields: append(append([]string{}, offsetEnvelopeFields...), "id", "sql_id", "sql_text", "db_name", "mysql_total", "query_time_avg", "query_time_max", "lock_time_avg", "rows_examined", "rows_sent_avg", "first_seen", "last_seen"), UntrustedFields: []string{"sql_text"}},
		"slowquery_history":  {Shape: "list", Fields: append(append(append([]string{}, offsetEnvelopeFields...), "sql_id"), "id", "sql_text", "db_name", "mysql_total", "query_time_avg", "query_time_max", "lock_time_avg", "rows_examined", "rows_sent_avg", "first_seen", "last_seen"), UntrustedFields: []string{"sql_text"}},
		"slowquery_optimize": {Shape: "object", Fields: []string{"tool", "result"}, UntrustedFields: []string{"result"}},

		// ─── binlog ───────────────────────────────────────────────────────────
		"binlog_list":  {Shape: "object", Fields: []string{"instance", "data"}, UntrustedFields: []string{"data"}},
		"binlog_parse": {Shape: "object", Fields: []string{"instance", "rollback", "count", "sqls"}, UntrustedFields: []string{"sqls"}},
		"binlog_purge": {Shape: "object", Fields: []string{"purged", "instance", "binlog"}},

		// ─── diagnostic ───────────────────────────────────────────────────────
		"diagnostic_row_list": {Shape: "array", Fields: []string{"<dynamic upstream diagnostic row keys>"}, UntrustedFields: []string{"Info", "info"}},
		"diagnostic_kill":     {Shape: "object", Fields: []string{"instance", "threads", "killed", "kill_sql"}, UntrustedFields: []string{"kill_sql"}},

		// ─── dict ─────────────────────────────────────────────────────────────
		// table_list returns data as a letter-keyed map of [name, comment] pairs
		// (get_group_tables_by_db); the CLI flattens it to {name, comment} rows.
		"dict_tables": {Shape: "object", Fields: []string{"instance", "db", "tables", "tables[].name", "tables[].comment", "count"}, UntrustedFields: []string{"tables[].comment"}},
		// table_info.data carries {column_list, rows} tables: meta_data, desc,
		// index, plus create_sql ([[name, ddl]], mysql only).
		"dict_table_info": {Shape: "object", Fields: []string{"instance", "db", "table", "info", "info.meta_data", "info.desc", "info.index", "info.create_sql"}, UntrustedFields: []string{"info"}},
		"dict_export":     {Shape: "object", Fields: []string{"instance", "db", "format", "content"}, UntrustedFields: []string{"content"}},
		// views/triggers/procedures have no route in v1.8.5; the command gates
		// with E_NOT_FOUND instead of issuing a request.
		"dict_unavailable": {Shape: "object", Fields: []string{"<gated: E_NOT_FOUND, no v1.8.5 route>"}},

		// ─── user ─────────────────────────────────────────────────────────────
		"user_list": {Shape: "list", Fields: append(append([]string{}, listEnvelopeFields...), "id", "username", "isActive", "display", "email", "isStaff")},
		// group_list has no session route in v1.8.5; it requires --mode jwt. In
		// session mode the command gates with E_NOT_FOUND instead of requesting.
		"group_list":          {Shape: "list", Fields: append(append([]string{}, listEnvelopeFields...), "id", "name"), UntrustedFields: []string{"name"}},
		"resource_group_list": {Shape: "list", Fields: append(append([]string{}, listEnvelopeFields...), "id", "name", "activeUsers", "bindUsers"), UntrustedFields: []string{"name"}},
		"resource_group_add":  {Shape: "object", Fields: []string{"groupId", "objectType", "added", "count"}},

		// ─── auth ─────────────────────────────────────────────────────────────
		"auth_login":  {Shape: "object", Fields: []string{"status", "region", "url", "message"}},
		"auth_logout": {Shape: "object", Fields: []string{"status", "region"}},
		"auth_status": {Shape: "object", Fields: []string{"configured", "region", "url", "hasTokens", "username"}},

		// ─── self-description ─────────────────────────────────────────────────
		"context":       {Shape: "object", Fields: []string{"version", "env", "defaultRegion", "activeRegion", "regions", "configPath", "credentials", "notices"}},
		"doctor":        {Shape: "object", Fields: []string{"version", "notices", "checks", "configExists", "authValid", "latencyMs", "region", "url", "username", "credentialStore", "error"}},
		"reference":     {Shape: "object", Fields: []string{"tool", "version", "security_tier", "release_readiness", "globalFlags", "global_flags", "exitCodes", "exit_codes", "errorCodes", "error_codes", "commands", "schemas"}},
		"changelog":     {Shape: "object", Fields: []string{"current_version", "since", "entries"}},
		"update_report": {Shape: "object", Fields: []string{"status", "asset", "channel", "current_version", "target_version", "update_available", "release_url", "install_method", "signature_status", "skill_sync_command", "skill_sync_status", "previous_version", "command", "preview", "confirm_token", "expires_at", "checksum_verified", "signature_verified", "path", "pending_path", "hint", "notices", "downgrade"}},
	}
}
