package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
)

// WorkflowAPI provides methods for the SQL workflow endpoints.
type WorkflowAPI struct {
	client *Client
}

// List returns a paginated list of SQL workflows.
//
// GET /api/v1/workflow/
func (w *WorkflowAPI) List(ctx context.Context, params WorkflowListParams) (*PaginatedResponse[SQLWorkflow], error) {
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

	data, err := w.client.Get(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("list workflows: %w", err)
	}

	var page PaginatedResponse[SQLWorkflow]
	if err := json.Unmarshal(data, &page); err != nil {
		return nil, fmt.Errorf("parsing workflow list: %w", err)
	}
	return &page, nil
}

// Submit creates a new SQL workflow.
//
// POST /api/v1/workflow/
func (w *WorkflowAPI) Submit(ctx context.Context, req WorkflowSubmitRequest) (*WorkflowSubmitResponse, error) {
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

// Detail fetches full workflow details including SQL content and audit log.
//
// It combines two API calls:
//   - GET /api/v1/workflow/{id}/ (REST, for metadata)
//   - GET /sqlworkflow/detail_content/?id={id} (internal, for SQL + audit log)
func (w *WorkflowAPI) Detail(ctx context.Context, id int) (*SQLWorkflowDetail, error) {
	// 1. Fetch REST metadata.
	data, err := w.client.Get(ctx, w.client.APIPath(fmt.Sprintf("/v1/workflow/%d/", id)))
	if err != nil {
		return nil, fmt.Errorf("workflow detail: %w", err)
	}

	var detail SQLWorkflowDetail
	if err := json.Unmarshal(data, &detail); err != nil {
		return nil, fmt.Errorf("parsing workflow detail: %w", err)
	}

	// 2. Fetch internal detail content (SQL + audit log).
	internalData, err := w.client.InternalGet(ctx, fmt.Sprintf("/sqlworkflow/detail_content/?id=%d", id))
	if err != nil {
		// Internal endpoint may not be available; return what we have.
		return &detail, nil
	}

	var internalResp InternalPaginatedResponse[sqlWorkflowContentRow]
	if err := json.Unmarshal(internalData, &internalResp); err == nil && internalResp.Status == 0 {
		if len(internalResp.Data.Rows) > 0 {
			row := internalResp.Data.Rows[0]
			if row.SQLContent != "" {
				detail.SQLContent = row.SQLContent
			}
			detail.AuditLog = row.AuditLog
		}
	}

	return &detail, nil
}

// sqlWorkflowContentRow is the internal API response shape for detail_content.
type sqlWorkflowContentRow struct {
	SQLContent string                   `json:"sql_content"`
	AuditLog   []SQLWorkflowAuditEntry  `json:"audit_log"`
}

// Audit approves or rejects a workflow.
//
// POST /api/v1/workflow/audit/
func (w *WorkflowAPI) Audit(ctx context.Context, req WorkflowAuditRequest) error {
	_, err := w.client.Post(ctx, w.client.APIPath("/v1/workflow/audit/"), req)
	if err != nil {
		return fmt.Errorf("audit workflow: %w", err)
	}
	return nil
}

// Execute starts execution of an approved workflow.
//
// POST /api/v1/workflow/execute/
func (w *WorkflowAPI) Execute(ctx context.Context, req WorkflowExecuteRequest) error {
	_, err := w.client.Post(ctx, w.client.APIPath("/v1/workflow/execute/"), req)
	if err != nil {
		return fmt.Errorf("execute workflow: %w", err)
	}
	return nil
}

// Cancel cancels a running workflow.
//
// POST /sqlworkflow/cancel/ (internal, form-encoded)
func (w *WorkflowAPI) Cancel(ctx context.Context, req WorkflowCancelRequest) error {
	form := url.Values{
		"workflow_id": {strconv.Itoa(req.WorkflowID),
		},
	}
	if req.Remark != "" {
		form.Set("remark", req.Remark)
	}

	_, err := w.client.InternalPost(ctx, "/sqlworkflow/cancel/", form)
	if err != nil {
		return fmt.Errorf("cancel workflow: %w", err)
	}
	return nil
}

// SQLCheck runs SQL syntax and risk checks.
//
// POST /api/v1/workflow/sqlcheck/
func (w *WorkflowAPI) SQLCheck(ctx context.Context, req WorkflowSQLCheckRequest) ([]SQLCheckResult, error) {
	data, err := w.client.Post(ctx, w.client.APIPath("/v1/workflow/sqlcheck/"), req)
	if err != nil {
		return nil, fmt.Errorf("sql check: %w", err)
	}

	// Archery returns {status: 0, data: [...]} or a direct array.
	var results []SQLCheckResult
	if err := json.Unmarshal(data, &results); err == nil {
		return results, nil
	}

	var wrapped InternalPaginatedResponse[SQLCheckResult]
	if err := json.Unmarshal(data, &wrapped); err == nil {
		return wrapped.Data.Rows, nil
	}

	// Try {status: 0, msg: "...", data: [...]} with data as raw array.
	var rawResp struct {
		Status int              `json:"status"`
		Msg    string           `json:"msg"`
		Data   json.RawMessage  `json:"data"`
	}
	if err := json.Unmarshal(data, &rawResp); err == nil && rawResp.Data != nil {
		_ = json.Unmarshal(rawResp.Data, &results)
	}

	return results, nil
}
