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

// SQLWorkflow is a single row from the workflow list endpoint.
type SQLWorkflow struct {
	ID         int    `json:"id"`
	Title      string `json:"workflow_name"`
	Status     int    `json:"status"`
	Engineer   string `json:"engineer"`
	InstanceID int    `json:"instance_id"`
	DBName     string `json:"db_name"`
	GroupID    int    `json:"group_id"`
	CreateDate string `json:"create_date"`
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
}

// SQLWorkflowAuditEntry is a single audit log entry within a workflow detail.
type SQLWorkflowAuditEntry struct {
	User       string `json:"audit_user"`
	Action     string `json:"audit_type"`
	Remark     string `json:"remark"`
	CreateDate string `json:"create_date"`
}

// WorkflowListParams holds query parameters for the workflow list endpoint.
type WorkflowListParams struct {
	Status     string
	Engineer   string
	InstanceID int
	DBName     string
	Limit      int
	Offset     int
}

// WorkflowSubmitRequest is the payload for POST /api/v1/workflow/.
type WorkflowSubmitRequest struct {
	Name       string `json:"workflow_name"`
	InstanceID int    `json:"instance_id"`
	DBName     string `json:"db_name"`
	SQLContent string `json:"sql_content"`
	GroupID    int    `json:"group_id,omitempty"`
	IsBackup   bool   `json:"is_backup"`
	DemandURL  string `json:"demand_url,omitempty"`
}

// WorkflowSubmitResponse is returned after a successful workflow submission.
type WorkflowSubmitResponse struct {
	ID int `json:"id"`
}

// WorkflowAuditRequest is the payload for POST /api/v1/workflow/audit/.
type WorkflowAuditRequest struct {
	WorkflowID int    `json:"workflow_id"`
	Action     string `json:"audit_type"`
	Remark     string `json:"remark,omitempty"`
}

// WorkflowExecuteRequest is the payload for POST /api/v1/workflow/execute/.
type WorkflowExecuteRequest struct {
	WorkflowID int    `json:"workflow_id"`
	Mode       string `json:"mode"`
}

// WorkflowCancelRequest is the payload for POST /sqlworkflow/cancel/.
type WorkflowCancelRequest struct {
	WorkflowID int    `json:"workflow_id"`
	Remark     string `json:"remark,omitempty"`
}

// WorkflowSQLCheckRequest is the payload for POST /api/v1/workflow/sqlcheck/.
type WorkflowSQLCheckRequest struct {
	InstanceID int    `json:"instance_id"`
	DBName     string `json:"db_name"`
	SQLContent string `json:"sql_content"`
}

// SQLCheckResult is a single item from the SQL check response.
type SQLCheckResult struct {
	Level        string `json:"level"`
	Message      string `json:"message"`
	AffectedRows int    `json:"affected_rows"`
	SQL          string `json:"sql"`
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

// BinlogFile represents a MySQL binlog file entry.
type BinlogFile struct {
	LogName  string `json:"log_name"`
	FileSize int64  `json:"file_size"`
}

// BinlogParseResult holds the parsed binlog events.
type BinlogParseResult struct {
	BinlogInfo string `json:"binlog_info"`
	SqlLog     string `json:"sql_log"`
}

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
