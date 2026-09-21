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
//	POST /api/v1/workflow/ (both transports)
//
// Archery deleted the /autoreview/ AJAX view — and its backing
// sql_workflow.submit — in v1.9.0, so from that version on the DRF endpoint is
// the only submit path. It exists in v1.8.5 too, which makes it the one route
// that works across every supported version. The transport mode therefore no
// longer picks the route, only how the request is authenticated.
func (w *WorkflowAPI) Submit(ctx context.Context, req WorkflowSubmitRequest) (*WorkflowSubmitResponse, error) {
	data, err := w.postAPI(ctx, "/v1/workflow/", req.toWire())
	if err != nil {
		return nil, fmt.Errorf("submit workflow: %w", err)
	}

	// A response we received but cannot decode is a contract failure, not a
	// network one: return it as an APIError (no status) so it maps to a
	// non-retryable E_UNKNOWN rather than the retryable E_NETWORK a plain error
	// would get.
	var env workflowSubmitEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, &APIError{ErrorMessages: []string{"parsing submit response: " + err.Error()}}
	}
	id := env.WorkflowID
	if id == 0 {
		id = env.Workflow.ID
	}
	if id == 0 {
		return nil, fmt.Errorf("submit workflow: server accepted the request but returned no workflow id")
	}
	return &WorkflowSubmitResponse{ID: id}, nil
}

// postAPI posts body to an Archery REST path with the active transport's
// credentials — the Django session cookie the CLI already holds, or the bearer
// token in jwt mode. Archery accepts both: SessionAuthentication sits beside
// JWTAuthentication in its DRF DEFAULT_AUTHENTICATION_CLASSES.
func (w *WorkflowAPI) postAPI(ctx context.Context, path string, body any) ([]byte, error) {
	full := w.client.APIPath(path)
	if w.client.Mode() == ModeJWT {
		return w.client.Post(ctx, full, body)
	}
	return w.client.SessionPostJSON(ctx, full, body)
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
//	POST /api/v1/workflow/sqlcheck/ (both transports)
//
// Like Submit, this moved off the AJAX view (/simplecheck/) that v1.9.0
// deleted. The DRF endpoint answers with ExecuteCheckResultSerializer, whose
// shape is byte-identical from v1.8.5 through v1.14 — the per-statement
// ReviewResult rows plus aggregate counters — so there is one envelope to
// parse, not a guess across several.
func (w *WorkflowAPI) SQLCheck(ctx context.Context, req WorkflowSQLCheckRequest) ([]SQLCheckResult, error) {
	data, err := w.postAPI(ctx, "/v1/workflow/sqlcheck/", req)
	if err != nil {
		return nil, fmt.Errorf("sql check: %w", err)
	}

	var env sqlCheckEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, &APIError{ErrorMessages: []string{"parsing sql check: " + err.Error()}}
	}

	results := make([]SQLCheckResult, len(env.Rows))
	for i, row := range env.Rows {
		results[i] = row.toSQLCheckResult()
	}
	return results, nil
}
