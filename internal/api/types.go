package api

import "encoding/json"

// ===== Paginated responses =====

// PaginatedResponse wraps DRF paginated list responses.
type PaginatedResponse[T any] struct {
	Count    int `json:"count"`
	Next     any `json:"next"`
	Previous any `json:"previous"`
	Results  []T `json:"results"`
}

// InternalPaginatedResponse wraps Archery internal paginated responses
// that use {status, msg, data: {total, rows}} format.
type InternalPaginatedResponse[T any] struct {
	Status int             `json:"status"`
	Msg    string          `json:"msg"`
	Data   InternalPage[T] `json:"data"`
}

// InternalPage holds the pagination payload for internal endpoints.
type InternalPage[T any] struct {
	Total int `json:"total"`
	Rows  []T `json:"rows"`
}

// ===== Workflow Definition =====

// Workflow represents an Archery workflow definition (template).
type Workflow struct {
	ID      int               `json:"id"`
	Name    string            `json:"name"`
	IsUsed  bool              `json:"is_used"`
	Content []WorkflowContent `json:"content"`
}

// WorkflowContent is a single step in a workflow definition.
type WorkflowContent struct {
	Step            int    `json:"step"`
	Assignee        int    `json:"assignee"`
	AuditAuthGroups string `json:"audit_auth_groups"`
}

// WorkflowAudit represents a pending workflow audit entry.
type WorkflowAudit struct {
	ID           int    `json:"id"`
	Title        string `json:"title"`
	Workflow     int    `json:"workflow"`
	WorkflowType int    `json:"workflow_type"`
	Status       int    `json:"status"`
	CurrentStep  int    `json:"current_step"`
	CreateUser   string `json:"create_user"`
	CreateDate   string `json:"create_date"`
	InstanceId   int    `json:"instance_id"`
}

// ===== SQL Workflow (submit / audit / execute) =====

// SQLWorkflow is a single row from the workflow list endpoint. It is the common
// shape both transports map into: the REST serializer fills the numeric fields
// (Status, InstanceID, GroupID), while the session projection fills StatusCode
// (the string status), InstanceName and GroupName. The unset side stays zero.
type SQLWorkflow struct {
	ID           int    `json:"id"`
	Title        string `json:"workflow_name"`
	Status       int    `json:"status"`
	StatusCode   string `json:"-"`
	Engineer     string `json:"engineer"`
	InstanceID   int    `json:"instance_id"`
	InstanceName string `json:"-"`
	DBName       string `json:"db_name"`
	GroupID      int    `json:"group_id"`
	GroupName    string `json:"-"`
	CreateDate   string `json:"create_date"`
}

// sessionWorkflowRow is a single row from the session AJAX list endpoint
// (/sqlworkflow_list/). Archery's _sql_workflow_list serializes a QuerySet
// .values(...) projection, so the JSON keys differ from the REST serializer:
// status is a string code (e.g. "workflow_finish"), the engineer column is
// engineer_display, instance is the joined instance__instance_name, and the
// timestamp is create_time. bigint_as_string=True renders id as a string.
type sessionWorkflowRow struct {
	ID           json.Number `json:"id"`
	Title        string      `json:"workflow_name"`
	StatusCode   string      `json:"status"`
	Engineer     string      `json:"engineer_display"`
	InstanceName string      `json:"instance__instance_name"`
	DBName       string      `json:"db_name"`
	GroupName    string      `json:"group_name"`
	IsBackup     bool        `json:"is_backup"`
	SyntaxType   int         `json:"syntax_type"`
	CreateTime   string      `json:"create_time"`
}

// toSQLWorkflow adapts a session list row to the common SQLWorkflow shape so the
// command/printing layer stays mode-agnostic. The session projection has no
// numeric instance_id (it joins the instance name instead), so InstanceID is
// left zero and InstanceName carries the human-readable value.
func (r sessionWorkflowRow) toSQLWorkflow() SQLWorkflow {
	id, _ := r.ID.Int64()
	return SQLWorkflow{
		ID:           int(id),
		Title:        r.Title,
		StatusCode:   r.StatusCode,
		Status:       WorkflowStatusCodeToInt(r.StatusCode),
		Engineer:     r.Engineer,
		InstanceName: r.InstanceName,
		DBName:       r.DBName,
		GroupName:    r.GroupName,
		CreateDate:   r.CreateTime,
	}
}

// WorkflowStatusCodeToInt maps Archery's session status string codes to the
// integer status used by the REST serializer and the CLI's status badges, so
// both transports render the same label. Unknown codes map to -1.
func WorkflowStatusCodeToInt(code string) int {
	switch code {
	case "workflow_manreviewing":
		return 1
	case "workflow_review_pass":
		return 2
	case "workflow_timingtask":
		return 4
	case "workflow_queuing", "workflow_executing":
		return 4
	case "workflow_finish":
		return 5
	case "workflow_exception":
		return 6
	case "workflow_abort":
		return 7
	case "workflow_autoreviewwrong":
		return 3
	default:
		return -1
	}
}

// SQLWorkflowDetail is the full detail of an SQL workflow.
type SQLWorkflowDetail struct {
	ID         int                     `json:"id"`
	Title      string                  `json:"workflow_name"`
	Status     int                     `json:"status"`
	Engineer   string                  `json:"engineer"`
	InstanceID int                     `json:"instance_id"`
	DBName     string                  `json:"db_name"`
	GroupID    int                     `json:"group_id"`
	SQLContent string                  `json:"sql_content"`
	IsBackup   bool                    `json:"is_backup"`
	DemandURL  string                  `json:"demand_url"`
	CreateDate string                  `json:"create_date"`
	AuditLog   []SQLWorkflowAuditEntry `json:"audit_log"`

	// 以下字段来自 session 路径的组合查询（detail_content + 列表），不来自单一 JSON 端点。
	// StatusCode 是字符串状态码（如 workflow_exception），从工单列表取，
	// 避免 detail 把缺省 status 误报成 0。
	StatusCode   string `json:"-"`
	InstanceName string `json:"-"`
	GroupName    string `json:"-"`
	// Result 是执行/审核结果行（含 stage、errlevel、errormessage、affected_rows），
	// 来自 /sqlworkflow/detail_content/，是「执行失败看原因」的关键数据。
	Result []reviewResultRow `json:"-"`
}

// SQLWorkflowAuditEntry is a single audit log entry within a workflow detail.
type SQLWorkflowAuditEntry struct {
	User       string `json:"audit_user"`
	Action     string `json:"audit_type"`
	Remark     string `json:"remark"`
	CreateDate string `json:"create_date"`
}

// WorkflowListParams holds query parameters for the workflow list endpoint.
//
// Session and REST transports accept overlapping but not identical filters. The
// session endpoint (/sqlworkflow_list/) filters on navStatus (the string status
// code), group_id, instance_id, search (matches engineer/title) and a
// start/end date range; it has no engineer or db_name filter. REST keeps the
// engineer/db_name filters it already supported. Audit narrows the list to the
// current user's pending-audit scope (/sqlworkflow_list_audit/).
type WorkflowListParams struct {
	Status     string
	Engineer   string
	InstanceID int
	GroupID    int
	DBName     string
	Search     string
	StartDate  string
	EndDate    string
	Limit      int
	Offset     int
	Audit      bool
}

// WorkflowSubmitRequest is the payload for submitting a workflow.
//
// The two transports key on different identifiers. The session endpoint
// (/autoreview/) takes names — InstanceName and GroupName — plus is_backup as
// the string "True"/"False"; the REST endpoint takes the numeric InstanceID and
// GroupID. The command fills whichever the active mode needs; the API layer
// reads the matching set. CCUsers and the RunDate* window are session-only
// optional fields (carbon-copy notify recipients, timed execution).
type WorkflowSubmitRequest struct {
	Name         string   `json:"workflow_name"`
	InstanceID   int      `json:"instance_id"`
	InstanceName string   `json:"-"`
	DBName       string   `json:"db_name"`
	SQLContent   string   `json:"sql_content"`
	GroupID      int      `json:"group_id,omitempty"`
	GroupName    string   `json:"-"`
	IsBackup     bool     `json:"is_backup"`
	DemandURL    string   `json:"demand_url,omitempty"`
	CCUsers      []string `json:"-"`
	RunDateStart string   `json:"-"`
	RunDateEnd   string   `json:"-"`
}

// WorkflowSubmitResponse is returned after a successful workflow submission.
type WorkflowSubmitResponse struct {
	ID int `json:"id"`
}

// WorkflowAuditRequest is the payload for POST /api/v1/workflow/audit/.
type WorkflowAuditRequest struct {
	WorkflowID int    `json:"workflow_id"`
	Action     string `json:"audit_type"`
	// Archery's AuditWorkflowSerializer names this audit_remark and treats it as
	// required; sending "remark" left it empty and the API rejected the audit.
	Remark string `json:"audit_remark"`
}

// WorkflowTypeSQLReview is Archery's workflow_type for SQL上线申请 (SQL-review
// workflows), the only type the execute command targets.
const WorkflowTypeSQLReview = 2

// WorkflowExecuteRequest is the payload for POST /api/v1/workflow/execute/.
//
// Archery's ExecuteWorkflowSerializer requires workflow_type and, for SQL-review
// workflows (type 2), engineer + mode — not just workflow_id. WorkflowType
// defaults to 2 (SQL上线申请); Engineer is the authenticated operator.
type WorkflowExecuteRequest struct {
	WorkflowID   int    `json:"workflow_id"`
	WorkflowType int    `json:"workflow_type"`
	Engineer     string `json:"engineer,omitempty"`
	Mode         string `json:"mode"`
}

// WorkflowCancelRequest is the payload for POST /sqlworkflow/cancel/.
type WorkflowCancelRequest struct {
	WorkflowID int    `json:"workflow_id"`
	Remark     string `json:"remark,omitempty"`
}

// WorkflowSQLCheckRequest is the payload for the SQL pre-check. Session
// (/simplecheck/) uses instance_name; REST uses instance_id. The command fills
// the identifier the active transport needs.
type WorkflowSQLCheckRequest struct {
	InstanceID   int    `json:"instance_id"`
	InstanceName string `json:"-"`
	DBName       string `json:"db_name"`
	SQLContent   string `json:"sql_content"`
}

// SQLCheckResult is a single item from the SQL check response. Level/Message
// are the REST serializer's fields; ErrLevel/StageStatus come from the session
// ReviewResult and drive auto-review classification (errlevel: 0 ok, 1 warning,
// 2 error). When the session transport fills the result, Level is derived from
// ErrLevel so existing badge rendering keeps working.
type SQLCheckResult struct {
	Level        string `json:"level"`
	Message      string `json:"message"`
	AffectedRows int    `json:"affected_rows"`
	SQL          string `json:"sql"`
	ErrLevel     int    `json:"errlevel"`
	StageStatus  string `json:"stagestatus,omitempty"`
}

// reviewResultRow is the session ReviewResult shape returned by /simplecheck/
// (data.rows) and /sqlworkflow/detail_content/ (rows). It mirrors Archery's
// ReviewResult.__dict__ projection.
type reviewResultRow struct {
	ID           int    `json:"id"`
	Stage        string `json:"stage"`
	ErrLevel     int    `json:"errlevel"`
	StageStatus  string `json:"stagestatus"`
	ErrorMessage string `json:"errormessage"`
	SQL          string `json:"sql"`
	AffectedRows int    `json:"affected_rows"`
	Sequence     string `json:"sequence"`
	BackupDBName string `json:"backup_dbname"`
	ExecuteTime  string `json:"execute_time"`
	SQLSha1      string `json:"sqlsha1"`
}

// toSQLCheckResult adapts a session ReviewResult into the common SQLCheckResult,
// deriving the textual level from errlevel so both transports render alike.
func (r reviewResultRow) toSQLCheckResult() SQLCheckResult {
	return SQLCheckResult{
		Level:        errLevelToString(r.ErrLevel),
		Message:      r.ErrorMessage,
		AffectedRows: r.AffectedRows,
		SQL:          r.SQL,
		ErrLevel:     r.ErrLevel,
		StageStatus:  r.StageStatus,
	}
}

// errLevelToString maps Archery's numeric errlevel to the CLI's level label.
func errLevelToString(errlevel int) string {
	switch errlevel {
	case 0:
		return "info"
	case 1:
		return "warning"
	default:
		return "error"
	}
}

// sessionCheckData is the data payload of /simplecheck/.
type sessionCheckData struct {
	Rows              []reviewResultRow `json:"rows"`
	CheckWarningCount int               `json:"CheckWarningCount"`
	CheckErrorCount   int               `json:"CheckErrorCount"`
}

// ===== Instance =====

// Instance represents a database instance configured in Archery.
type Instance struct {
	ID           json.Number `json:"id"`
	InstanceName string      `json:"instance_name"`
	DbType       string      `json:"db_type"`
	Host         string      `json:"host"`
	Port         int         `json:"port"`
	User         string      `json:"user"`
	Password     string      `json:"password"`
	DBName       string      `json:"db_name"`
	Charset      string      `json:"charset"`
	IsActive     bool        `json:"is_active"`
	Environment  string      `json:"environment"`
	// Archery's instance_tag is a ManyToMany relation serialized as an array,
	// not a string (empty M2M -> []).
	InstanceTag []any `json:"instance_tag"`
}

// ===== Query =====

// QueryResult holds the result of a SQL query execution.
type QueryResult struct {
	Rows         []map[string]any `json:"rows"`
	ColumnNames  []string         `json:"column_names"`
	RowCount     int              `json:"row_count"`
	AffectedRows int              `json:"affected_rows"`
	IsExecute    bool             `json:"is_execute"`
	ErrorMessage string           `json:"error_message"`
}

// QueryLog represents a single query log entry.
type QueryLog struct {
	ID         int    `json:"id"`
	Username   string `json:"username"`
	DbUser     string `json:"db_user"`
	SQLText    string `json:"sqllog"`
	EffectRows int    `json:"effect_row"`
	CostTime   string `json:"cost_time"`
	Instance   string `json:"instance_name"`
	ExecTime   string `json:"exec_time"`
}

// ===== Slow Query =====

// SlowQuery represents a slow query entry from the analysis system.
type SlowQuery struct {
	ID           int     `json:"id"`
	SQLText      string  `json:"sql_text"`
	MySQLTotal   float64 `json:"mysql_total"`
	Count        int     `json:"count"`
	MaxQueryTime float64 `json:"max_query_time"`
	MysqlMaxTime float64 `json:"mysql_max_time"`
}

// SlowQueryHistory represents historical slow query data.
type SlowQueryHistory struct {
	ID           int     `json:"id"`
	SlowQueryID  int     `json:"slowquery_id"`
	SQLText      string  `json:"sql_text"`
	StartTime    string  `json:"start_time"`
	StopTime     string  `json:"stop_time"`
	QueryTimeAvg float64 `json:"query_time_avg"`
	LockTimeAvg  float64 `json:"lock_time_avg"`
	RowsExamined int     `json:"rows_examined_avg"`
	RowsSent     int     `json:"rows_sent_avg"`
}

// ===== DBA Tools =====

// ProcessInfo represents a MySQL process list entry.
type ProcessInfo struct {
	ID      int    `json:"id"`
	User    string `json:"user"`
	Host    string `json:"host"`
	DB      string `json:"db"`
	Command string `json:"command"`
	Time    int    `json:"time"`
	State   string `json:"state"`
	Info    string `json:"info"`
}

// TableSpace represents table space usage information.
type TableSpace struct {
	TableName string `json:"table_name"`
	Engine    string `json:"engine"`
	RowFormat string `json:"row_format"`
	Rows      int    `json:"table_rows"`
	DataSize  string `json:"data_length"`
	IndexSize string `json:"index_length"`
	TotalSize string `json:"total_size"`
}

// LockInfo represents an InnoDB lock entry.
type LockInfo struct {
	LockTrxID  string `json:"lock_trx_id"`
	LockMode   string `json:"lock_mode"`
	LockType   string `json:"lock_type"`
	LockTable  string `json:"lock_table"`
	LockIndex  string `json:"lock_index"`
	LockSpace  int    `json:"lock_space"`
	LockPage   int    `json:"lock_page"`
	LockRec    int    `json:"lock_rec"`
	LockData   string `json:"lock_data"`
	TrxID      string `json:"trx_id"`
	TrxState   string `json:"trx_state"`
	TrxStarted string `json:"trx_started"`
	TrxQuery   string `json:"trx_query"`
	TrxWait    int    `json:"trx_wait"`
}

// TransactionInfo represents a running transaction.
type TransactionInfo struct {
	TrxID           string `json:"trx_id"`
	TrxState        string `json:"trx_state"`
	TrxStarted      string `json:"trx_started"`
	TrxQuery        string `json:"trx_query"`
	TrxRowsLocked   int    `json:"trx_rows_locked"`
	TrxRowsModified int    `json:"trx_rows_modified"`
}

// ===== Binlog =====
//
// Binlog request/response types live in binlog.go alongside the BinlogAPI.

// ===== Archive =====

// ArchiveConfig represents an archive task configuration.
type ArchiveConfig struct {
	ID            int    `json:"id"`
	Title         string `json:"title"`
	AuditWorkflow int    `json:"audit_workflow"`
	AuditUsers    string `json:"audit_users"`
	SourceHost    string `json:"source_host"`
	SourceDb      string `json:"source_db"`
	SourceTable   string `json:"source_table"`
	TargetHost    string `json:"target_host"`
	TargetDb      string `json:"target_db"`
	TargetTable   string `json:"target_table"`
	Mode          string `json:"mode"`
	Condition     string `json:"condition"`
	IsArchive     bool   `json:"is_archive"`
	ExecDate      string `json:"exec_date"`
	Status        int    `json:"status"`
}

// ArchiveLog represents a single archive execution log.
type ArchiveLog struct {
	ID          int    `json:"id"`
	ConfigID    int    `json:"config_id"`
	SQLText     string `json:"sql"`
	ExecTime    string `json:"exec_time"`
	Status      int    `json:"status"`
	RowsMoved   int    `json:"rows_moved"`
	ElapsedTime string `json:"elapsed_time"`
}

// ===== Data Dictionary =====

// DataDictionaryTable represents a table entry in the data dictionary.
type DataDictionaryTable struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Comment     string `json:"comment"`
	InstanceID  int    `json:"instance_id"`
	DBName      string `json:"db_name"`
	TableName   string `json:"table_name"`
	IsPublished bool   `json:"is_published"`
}

// DataDictionaryTableInfo holds detailed column info for a dictionary table.
type DataDictionaryTableInfo struct {
	TableName string                 `json:"table_name"`
	Comment   string                 `json:"comment"`
	Columns   []DataDictionaryColumn `json:"columns"`
}

// DataDictionaryColumn describes a single column in a data dictionary table.
type DataDictionaryColumn struct {
	ColumnName    string `json:"column_name"`
	ColumnType    string `json:"column_type"`
	ColumnComment string `json:"column_comment"`
	IsNullable    string `json:"is_nullable"`
	ColumnKey     string `json:"column_key"`
	ColumnDefault any    `json:"column_default"`
}

// ===== User & Group =====

// User represents an Archery user.
type User struct {
	ID       int    `json:"id"`
	Username string `json:"username"`
	Display  string `json:"display"`
	Email    string `json:"email"`
	IsActive bool   `json:"is_active"`
	IsStaff  bool   `json:"is_staff"`
	Groups   []int  `json:"groups"`
}

// Group represents an Archery permission group.
type Group struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// ResourceGroup represents a resource group for instance access control.
type ResourceGroup struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Users []int  `json:"users"`
}
