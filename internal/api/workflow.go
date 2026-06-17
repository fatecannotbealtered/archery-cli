package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// WorkflowAPI provides methods for the SQL workflow endpoints.
//
// Each method branches on the client transport mode. Session mode (the default)
// targets Archery's AJAX/Django endpoints, which ordinary accounts can reach;
// JWT mode targets the REST API under /api/v1/. The session shapes are taken
// from sql/sql_workflow.py in the v1.8.5 source.
type WorkflowAPI struct {
	client *Client
}

// ListResult holds the parsed response and pagination metadata for a list call.
type ListResult[T any] struct {
	Page       PaginatedResponse[T]
	Pagination Pagination
}

// List returns a paginated list of SQL workflows.
//
//	session: POST /sqlworkflow_list/ (or /sqlworkflow_list_audit/ when Audit)
//	jwt:     GET  /api/v1/workflow/
func (w *WorkflowAPI) List(ctx context.Context, params WorkflowListParams) (*ListResult[SQLWorkflow], error) {
	if w.client.Mode() == ModeJWT {
		return w.listREST(ctx, params)
	}
	return w.listSession(ctx, params)
}

// listSession queries the AJAX list endpoint. The view reads navStatus,
// instance_id, group_id, search, start_date/end_date and limit/offset from POST
// form fields and returns {"total":N,"rows":[...]}. Note the server computes
// its slice as workflow[offset : offset+limit], so limit is the page size.
func (w *WorkflowAPI) listSession(ctx context.Context, params WorkflowListParams) (*ListResult[SQLWorkflow], error) {
	form := url.Values{}
	form.Set("limit", strconv.Itoa(params.Limit))
	form.Set("offset", strconv.Itoa(params.Offset))
	// navStatus filters by the string status code; "all" (or empty) means no
	// status filter. The view treats any falsy navStatus as no filter.
	if params.Status != "" {
		form.Set("navStatus", params.Status)
	}
	if params.InstanceID > 0 {
		form.Set("instance_id", strconv.Itoa(params.InstanceID))
	}
	if params.GroupID > 0 {
		form.Set("group_id", strconv.Itoa(params.GroupID))
	}
	if params.Search != "" {
		form.Set("search", params.Search)
	}
	if params.StartDate != "" && params.EndDate != "" {
		form.Set("start_date", params.StartDate)
		form.Set("end_date", params.EndDate)
	}

	endpoint := "/sqlworkflow_list/"
	if params.Audit {
		endpoint = "/sqlworkflow_list_audit/"
	}

	data, err := w.client.SessionPost(ctx, endpoint, form)
	if err != nil {
		return nil, fmt.Errorf("list workflows: %w", err)
	}

	var raw struct {
		Total int                  `json:"total"`
		Rows  []sessionWorkflowRow `json:"rows"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing workflow list: %w", err)
	}

	results := make([]SQLWorkflow, len(raw.Rows))
	for i, row := range raw.Rows {
		results[i] = row.toSQLWorkflow()
	}

	return &ListResult[SQLWorkflow]{
		Page: PaginatedResponse[SQLWorkflow]{Count: raw.Total, Results: results},
		Pagination: Pagination{
			Count:   raw.Total,
			PerPage: params.Limit,
		},
	}, nil
}

// listREST queries the JWT REST list endpoint (DRF pagination).
func (w *WorkflowAPI) listREST(ctx context.Context, params WorkflowListParams) (*ListResult[SQLWorkflow], error) {
	q := url.Values{}
	if params.Status != "" {
		q.Set("status", params.Status)
	}
	if params.Engineer != "" {
		q.Set("engineer", params.Engineer)
	}
	if params.InstanceID > 0 {
		q.Set("instance_id", strconv.Itoa(params.InstanceID))
	}
	if params.DBName != "" {
		q.Set("db_name", params.DBName)
	}
	if params.Limit > 0 {
		q.Set("limit", strconv.Itoa(params.Limit))
	}
	if params.Offset > 0 {
		q.Set("offset", strconv.Itoa(params.Offset))
	}

	path := w.client.APIPath("/v1/workflow/")
	if encoded := q.Encode(); encoded != "" {
		path += "?" + encoded
	}

	data, headerPag, err := w.client.GetWithPagination(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("list workflows: %w", err)
	}

	var page PaginatedResponse[SQLWorkflow]
	if err := json.Unmarshal(data, &page); err != nil {
		return nil, fmt.Errorf("parsing workflow list: %w", err)
	}

	pag := headerPag
	if pag.Count == 0 {
		pag.Count = page.Count
	}

	return &ListResult[SQLWorkflow]{Page: page, Pagination: pag}, nil
}

// Submit creates a new SQL workflow.
//
//	session: POST /autoreview/ (names; is_backup as "True"/"False"; 302 redirect)
//	jwt:     POST /api/v1/workflow/
func (w *WorkflowAPI) Submit(ctx context.Context, req WorkflowSubmitRequest) (*WorkflowSubmitResponse, error) {
	if w.client.Mode() == ModeJWT {
		return w.submitREST(ctx, req)
	}
	return w.submitSession(ctx, req)
}

// submitSession posts the workflow form to /autoreview/. On success the view
// returns a 302 redirect to /detail/<id>/; the workflow id is parsed from the
// Location header. On a validation failure it renders error.html (HTTP 200);
// the redirect-absent case surfaces as an error.
func (w *WorkflowAPI) submitSession(ctx context.Context, req WorkflowSubmitRequest) (*WorkflowSubmitResponse, error) {
	form := url.Values{}
	form.Set("sql_content", req.SQLContent)
	form.Set("workflow_name", req.Name)
	form.Set("demand_url", req.DemandURL)
	form.Set("group_name", req.GroupName)
	form.Set("instance_name", req.InstanceName)
	form.Set("db_name", req.DBName)
	form.Set("is_backup", boolToPyString(req.IsBackup))
	for _, u := range req.CCUsers {
		form.Add("cc_users", u)
	}
	if req.RunDateStart != "" {
		form.Set("run_date_start", req.RunDateStart)
	}
	if req.RunDateEnd != "" {
		form.Set("run_date_end", req.RunDateEnd)
	}

	id, err := w.client.SessionPostExpectRedirect(ctx, "/autoreview/", form)
	if err != nil {
		return nil, fmt.Errorf("submit workflow: %w", err)
	}
	return &WorkflowSubmitResponse{ID: id}, nil
}

// submitREST posts the workflow to the JWT REST endpoint.
func (w *WorkflowAPI) submitREST(ctx context.Context, req WorkflowSubmitRequest) (*WorkflowSubmitResponse, error) {
	data, err := w.client.Post(ctx, w.client.APIPath("/v1/workflow/"), req)
	if err != nil {
		return nil, fmt.Errorf("submit workflow: %w", err)
	}

	var resp WorkflowSubmitResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parsing submit response: %w", err)
	}
	if resp.ID == 0 {
		// Some Archery versions return the ID in a different shape.
		var raw map[string]any
		if json.Unmarshal(data, &raw) == nil {
			if id, ok := raw["id"].(float64); ok && id > 0 {
				resp.ID = int(id)
			}
		}
	}
	return &resp, nil
}

// Detail fetches workflow details. v1.8.5 has no REST detail endpoint
// (/api/v1/workflow/<id>/ -> 404), so both modes read the session view.
//
//	GET /sqlworkflow/detail_content/?workflow_id=<id>
//
// The view returns {"rows":[ReviewResult...]} — the SQL check / execution
// result rows, not workflow metadata (no such metadata endpoint exists in
// v1.8.5). SQL content is reconstructed from the rows. List rows carry the
// metadata (title/status/engineer) when the caller has them.
func (w *WorkflowAPI) Detail(ctx context.Context, id int) (*SQLWorkflowDetail, error) {
	data, err := w.client.SessionGet(ctx, fmt.Sprintf("/sqlworkflow/detail_content/?workflow_id=%d", id))
	if err != nil {
		return nil, fmt.Errorf("workflow detail: %w", err)
	}

	var resp struct {
		Rows []reviewResultRow `json:"rows"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		// A response we received but cannot decode is a contract/parse failure,
		// not a network problem. Return it as an APIError (no status) so it maps
		// to a non-retryable E_UNKNOWN instead of the retryable E_NETWORK the
		// plain-error fallback would assign.
		return nil, &APIError{ErrorMessages: []string{"parsing workflow detail: " + err.Error()}}
	}

	detail := &SQLWorkflowDetail{ID: id}
	// detail_content 的 rows 是执行/审核结果(含 errlevel/stagestatus/errormessage)。
	// 全部保留到 Result —— 这是「执行失败看原因」的关键。同时从 rows 重建真实 SQL,
	// 但过滤掉 inception 的包装语句(inception_magic_start/commit),它们不是用户 SQL。
	detail.Result = resp.Rows
	for _, row := range resp.Rows {
		sqlText := strings.TrimSpace(row.SQL)
		if sqlText == "" || strings.HasPrefix(sqlText, "inception_magic") {
			continue
		}
		if detail.SQLContent != "" {
			detail.SQLContent += "\n"
		}
		detail.SQLContent += sqlText
	}

	// 元信息(title/status/engineer/db/group)在 session 模式没有 by-id JSON 端点
	// (详情页是 HTML)。从工单列表里按 id 匹配补齐;拿不到则只缺元信息,执行结果照常。
	if list, lerr := w.listSession(ctx, WorkflowListParams{Limit: 500}); lerr == nil {
		for _, wf := range list.Page.Results {
			if wf.ID == id {
				detail.Title = wf.Title
				detail.Status = wf.Status
				detail.StatusCode = wf.StatusCode
				detail.Engineer = wf.Engineer
				detail.InstanceID = wf.InstanceID
				detail.InstanceName = wf.InstanceName
				detail.DBName = wf.DBName
				detail.GroupID = wf.GroupID
				detail.GroupName = wf.GroupName
				detail.CreateDate = wf.CreateDate
				break
			}
		}
	}
	return detail, nil
}

// Audit approves a workflow (audit pass). Cancel/reject goes through Cancel.
//
//	session: POST /passed/ (workflow_id + audit_remark; requires auditor)
//	jwt:     POST /api/v1/workflow/audit/
func (w *WorkflowAPI) Audit(ctx context.Context, req WorkflowAuditRequest) error {
	// "cancel" actions are routed through the cancel view, not /passed/.
	if req.Action == "cancel" {
		return w.Cancel(ctx, WorkflowCancelRequest{WorkflowID: req.WorkflowID, Remark: req.Remark})
	}
	if w.client.Mode() == ModeJWT {
		_, err := w.client.Post(ctx, w.client.APIPath("/v1/workflow/audit/"), req)
		if err != nil {
			return fmt.Errorf("audit workflow: %w", err)
		}
		return nil
	}

	form := url.Values{
		"workflow_id":  {strconv.Itoa(req.WorkflowID)},
		"audit_remark": {req.Remark},
	}
	// The view returns a 302 redirect on success and renders error.html (200)
	// on a permission/validation failure; either is a non-4xx body here.
	if _, err := w.client.SessionPost(ctx, "/passed/", form); err != nil {
		return fmt.Errorf("audit workflow: %w", err)
	}
	return nil
}

// Execute starts execution of an approved workflow.
//
//	session: POST /execute/ (workflow_id + mode; requires execute permission)
//	jwt:     POST /api/v1/workflow/execute/
func (w *WorkflowAPI) Execute(ctx context.Context, req WorkflowExecuteRequest) error {
	if w.client.Mode() == ModeJWT {
		_, err := w.client.Post(ctx, w.client.APIPath("/v1/workflow/execute/"), req)
		if err != nil {
			return fmt.Errorf("execute workflow: %w", err)
		}
		return nil
	}

	form := url.Values{
		"workflow_id": {strconv.Itoa(req.WorkflowID)},
		"mode":        {req.Mode},
	}
	if _, err := w.client.SessionPost(ctx, "/execute/", form); err != nil {
		return fmt.Errorf("execute workflow: %w", err)
	}
	return nil
}

// Cancel terminates a workflow.
//
//	session: POST /cancel/ (workflow_id + cancel_remark)
//
// The session field is cancel_remark and the view requires it to be present
// (a nil value renders "终止原因不能为空"). There is no REST cancel endpoint.
func (w *WorkflowAPI) Cancel(ctx context.Context, req WorkflowCancelRequest) error {
	form := url.Values{
		"workflow_id":   {strconv.Itoa(req.WorkflowID)},
		"cancel_remark": {req.Remark},
	}
	if _, err := w.client.SessionPost(ctx, "/cancel/", form); err != nil {
		return fmt.Errorf("cancel workflow: %w", err)
	}
	return nil
}

// SQLCheck runs SQL syntax and risk checks without creating a workflow.
//
//	session: POST /simplecheck/ (instance_name + db_name + sql_content)
//	jwt:     POST /api/v1/workflow/sqlcheck/
func (w *WorkflowAPI) SQLCheck(ctx context.Context, req WorkflowSQLCheckRequest) ([]SQLCheckResult, error) {
	if w.client.Mode() == ModeJWT {
		return w.sqlCheckREST(ctx, req)
	}
	return w.sqlCheckSession(ctx, req)
}

// sqlCheckSession posts to /simplecheck/. The view returns
// {"status":0,"msg":"ok","data":{"rows":[ReviewResult],"CheckWarningCount":N,
// "CheckErrorCount":N}}; status!=0 carries the engine error (e.g. instance not
// found) and is surfaced as an error.
func (w *WorkflowAPI) sqlCheckSession(ctx context.Context, req WorkflowSQLCheckRequest) ([]SQLCheckResult, error) {
	form := url.Values{
		"sql_content":   {req.SQLContent},
		"instance_name": {req.InstanceName},
		"db_name":       {req.DBName},
	}
	data, err := w.client.SessionPost(ctx, "/simplecheck/", form)
	if err != nil {
		return nil, fmt.Errorf("sql check: %w", err)
	}

	var resp struct {
		Status int              `json:"status"`
		Msg    string           `json:"msg"`
		Data   sessionCheckData `json:"data"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parsing sql check: %w", err)
	}
	if resp.Status != 0 {
		return nil, &APIError{StatusCode: 400, ErrorMessages: []string{resp.Msg}}
	}

	results := make([]SQLCheckResult, len(resp.Data.Rows))
	for i, row := range resp.Data.Rows {
		results[i] = row.toSQLCheckResult()
	}
	return results, nil
}

// sqlCheckREST posts to the JWT REST sqlcheck endpoint, tolerating the several
// response envelopes older Archery versions return.
func (w *WorkflowAPI) sqlCheckREST(ctx context.Context, req WorkflowSQLCheckRequest) ([]SQLCheckResult, error) {
	data, err := w.client.Post(ctx, w.client.APIPath("/v1/workflow/sqlcheck/"), req)
	if err != nil {
		return nil, fmt.Errorf("sql check: %w", err)
	}

	var results []SQLCheckResult
	if err := json.Unmarshal(data, &results); err == nil {
		return results, nil
	}

	var wrapped InternalPaginatedResponse[SQLCheckResult]
	if err := json.Unmarshal(data, &wrapped); err == nil {
		return wrapped.Data.Rows, nil
	}

	var rawResp struct {
		Status int             `json:"status"`
		Msg    string          `json:"msg"`
		Data   json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(data, &rawResp); err == nil && rawResp.Data != nil {
		_ = json.Unmarshal(rawResp.Data, &results)
	}

	return results, nil
}

// boolToPyString renders a Go bool the way Archery's submit view expects:
// is_backup is compared against the literal "True".
func boolToPyString(b bool) string {
	if b {
		return "True"
	}
	return "False"
}
