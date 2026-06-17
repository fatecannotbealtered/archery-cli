package cmd

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/fatecannotbealtered/archery-cli/internal/api"
	"github.com/fatecannotbealtered/archery-cli/internal/output"
)

// ─── List printing ──────────────────────────────────────────────────────────

func printWorkflowListJSON(result *api.ListResult[api.SQLWorkflow], fields []string, host string) {
	items := make([]map[string]any, len(result.Page.Results))
	for i, w := range result.Page.Results {
		items[i] = workflowToMap(w, host)
	}
	if len(fields) > 0 {
		filtered := make([]map[string]any, len(items))
		for i, m := range items {
			filtered[i] = output.FilterMap(m, fields)
		}
		items = filtered
	}
	pag := result.Pagination
	hasMore := pag.NextPage > 0 || (result.Page.Next != nil && result.Page.Next != "")
	output.PrintJSON(output.NewListEnvelope(items, output.ListMeta{
		Count:   len(result.Page.Results),
		Limit:   pag.PerPage,
		Page:    pag.Page,
		Total:   pag.Count,
		HasMore: hasMore,
	}))
}

// printListPaginationHint prints a summary line below the table showing
// pagination state. Only shown in terminal (non-JSON) mode when there is
// more data available or when the total count is known.
func printListPaginationHint(pag api.Pagination, requestedLimit int) {
	total := pag.Count
	showing := requestedLimit
	if showing <= 0 {
		showing = total
	}

	// Determine if there are more pages.
	hasMore := pag.NextPage > 0
	if !hasMore && total > 0 && showing > 0 && total > showing {
		hasMore = true
	}

	if total > 0 {
		msg := fmt.Sprintf("Showing %d of %d results", showing, total)
		if hasMore {
			msg += fmt.Sprintf(" (use --offset %d for next page)", showing)
		}
		output.Gray("  " + msg)
	} else if hasMore {
		output.Gray(fmt.Sprintf("  More results available (use --offset %d for next page)", showing))
	}
}

func printWorkflowTable(workflows []api.SQLWorkflow, host string) {
	headers := []string{"ID", "TITLE", "STATUS", "ENGINEER", "INSTANCE", "DB", "CREATED"}
	rows := make([][]string, len(workflows))
	for i, w := range workflows {
		title := w.Title
		if len(title) > 40 {
			title = title[:37] + "..."
		}
		// Session rows have an instance name; REST rows have a numeric id.
		instance := w.InstanceName
		if instance == "" {
			instance = fmt.Sprintf("%d", w.InstanceID)
		}
		rows[i] = []string{
			fmt.Sprintf("%d", w.ID),
			title,
			workflowStatusBadge(w.Status),
			w.Engineer,
			instance,
			w.DBName,
			formatTime(w.CreateDate),
		}
	}
	output.Table(headers, rows)
}

// ─── Detail printing ────────────────────────────────────────────────────────

func printWorkflowDetailJSON(detail *api.SQLWorkflowDetail, fields []string, host string) {
	m := workflowDetailToMap(detail, host)
	if len(fields) > 0 {
		m = output.FilterMap(m, fields)
	}
	output.PrintJSON(m)
}

func printWorkflowDetail(detail *api.SQLWorkflowDetail, host string) {
	fmt.Println()
	header := fmt.Sprintf("  %s · %s",
		output.FormatCyanBold(fmt.Sprintf("#%d", detail.ID)),
		workflowStatusBadge(detail.Status))
	fmt.Println(header)
	output.Gray("  ────────────────────────────────────────────────")

	printField := func(label, value string) {
		if value != "" {
			fmt.Printf("  %-14s %s\n", output.FormatGray(label), value)
		}
	}

	printField("Title", detail.Title)
	printField("Engineer", detail.Engineer)
	printField("Status", workflowStatusLabel(detail.Status))
	printField("Instance", fmt.Sprintf("%d", detail.InstanceID))
	printField("Database", detail.DBName)
	printField("Group", fmt.Sprintf("%d", detail.GroupID))
	printField("Created", formatTime(detail.CreateDate))
	if detail.DemandURL != "" {
		printField("Demand URL", detail.DemandURL)
	}
	printField("URL", host+fmt.Sprintf("/sqlworkflow/%d/", detail.ID))

	if detail.SQLContent != "" {
		fmt.Println()
		output.Gray("  SQL Content")
		output.Gray("  ────────────────────────────────────────────────")
		sql := detail.SQLContent
		if len(sql) > 2000 {
			sql = sql[:2000] + "\n  ... (truncated)"
		}
		for _, line := range strings.Split(sql, "\n") {
			fmt.Println("  " + line)
		}
	}

	if len(detail.AuditLog) > 0 {
		fmt.Println()
		output.Gray("  Audit Log")
		output.Gray("  ────────────────────────────────────────────────")
		for _, entry := range detail.AuditLog {
			action := entry.Action
			if action == "" {
				action = "review"
			}
			remark := ""
			if entry.Remark != "" {
				remark = fmt.Sprintf(" (%s)", entry.Remark)
			}
			fmt.Printf("  %s  %s -> %s%s\n",
				output.FormatGray(formatTime(entry.CreateDate)),
				output.FormatCyanBold(entry.User),
				action,
				remark)
		}
	}
	fmt.Println()
}

// ─── SQLCheck printing ──────────────────────────────────────────────────────

func printSQLCheckResults(results []api.SQLCheckResult) {
	if len(results) == 0 {
		output.Success("SQL check passed with no issues.")
		return
	}

	headers := []string{"LEVEL", "MESSAGE", "AFFECTED_ROWS", "SQL"}
	rows := make([][]string, len(results))
	for i, r := range results {
		sql := r.SQL
		if len(sql) > 60 {
			sql = sql[:57] + "..."
		}
		rows[i] = []string{
			checkLevelBadge(r.Level),
			r.Message,
			fmt.Sprintf("%d", r.AffectedRows),
			sql,
		}
	}
	output.Table(headers, rows)

	hasError := false
	for _, r := range results {
		if r.Level == "error" {
			hasError = true
			break
		}
	}
	if hasError {
		output.Warn("SQL check found errors. Fix them before submitting.")
	} else {
		output.Warn("SQL check found warnings. Review before submitting.")
	}
}

// ─── Map converters for JSON output ─────────────────────────────────────────

func workflowToMap(w api.SQLWorkflow, host string) map[string]any {
	m := map[string]any{
		"id":         strconv.Itoa(w.ID),
		"title":      w.Title,
		"status":     w.Status,
		"engineer":   w.Engineer,
		"instanceId": strconv.Itoa(w.InstanceID),
		"db":         w.DBName,
		"created":    w.CreateDate,
	}
	// Session list rows carry the raw status code and human-readable
	// instance/group names; surface them when present.
	if w.StatusCode != "" {
		m["statusCode"] = w.StatusCode
	}
	if w.InstanceName != "" {
		m["instance"] = w.InstanceName
	}
	if w.GroupName != "" {
		m["group"] = w.GroupName
	}
	if host != "" {
		m["webUrl"] = host + fmt.Sprintf("/sqlworkflow/%d/", w.ID)
	}
	api.TagUntrusted(m, "title")
	return normalizeAgentMap(m)
}

func workflowDetailToMap(d *api.SQLWorkflowDetail, host string) map[string]any {
	m := map[string]any{
		"id":         strconv.Itoa(d.ID),
		"title":      d.Title,
		"status":     d.Status,
		"engineer":   d.Engineer,
		"instanceId": strconv.Itoa(d.InstanceID),
		"db":         d.DBName,
		"groupId":    strconv.Itoa(d.GroupID),
		"sql":        d.SQLContent,
		"created":    d.CreateDate,
	}
	// 字符串状态码(如 workflow_exception) —— 比裸 int 可读, 也修掉 detail 误报 status:0。
	if d.StatusCode != "" {
		m["statusCode"] = d.StatusCode
	}
	if d.InstanceName != "" {
		m["instance"] = d.InstanceName
	}
	if d.GroupName != "" {
		m["group"] = d.GroupName
	}
	// 执行/审核结果 —— 执行失败时这里带 errormessage(如 "Invalid remote backup information")，
	// 让开发者从 CLI 就能看到失败原因，而不必去网页/DB 查。
	if len(d.Result) > 0 {
		rows := make([]map[string]any, 0, len(d.Result))
		for _, r := range d.Result {
			row := map[string]any{
				"stage":       r.Stage,
				"stageStatus": r.StageStatus,
				"errLevel":    r.ErrLevel,
			}
			if r.ErrorMessage != "" {
				row["error"] = r.ErrorMessage
			}
			if r.AffectedRows != 0 {
				row["affectedRows"] = r.AffectedRows
			}
			if r.ExecuteTime != "" && r.ExecuteTime != "0" {
				row["executeTime"] = string(r.ExecuteTime)
			}
			rows = append(rows, row)
		}
		m["result"] = rows
	}
	if d.DemandURL != "" {
		m["demandUrl"] = d.DemandURL
	}
	if host != "" {
		m["webUrl"] = host + fmt.Sprintf("/sqlworkflow/%d/", d.ID)
	}
	if len(d.AuditLog) > 0 {
		logs := make([]map[string]any, len(d.AuditLog))
		for i, e := range d.AuditLog {
			logs[i] = map[string]any{
				"user":      e.User,
				"action":    e.Action,
				"remark":    e.Remark,
				"timestamp": e.CreateDate,
			}
		}
		m["auditLog"] = logs
	}
	// Tag externally-sourced fields as untrusted (SEC-SPEC §2).
	api.TagUntrusted(m, "title", "sql", "auditLog", "demandUrl", "result")
	return normalizeAgentMap(m)
}

// ─── Status display helpers ─────────────────────────────────────────────────

func workflowStatusBadge(status int) string {
	label := workflowStatusLabel(status)
	switch status {
	case 0:
		return output.StatusBadge("created")
	case 1:
		return output.StatusBadge("pending")
	case 2:
		return output.StatusBadge("approved")
	case 3:
		return output.StatusBadge("rejected")
	case 4:
		return output.StatusBadge("executing")
	case 5:
		return output.StatusBadge("success")
	case 6:
		return output.StatusBadge("failed")
	case 7:
		return output.StatusBadge("cancelled")
	default:
		return output.StatusBadge(label)
	}
}

func workflowStatusLabel(status int) string {
	switch status {
	case 0:
		return "created"
	case 1:
		return "pending"
	case 2:
		return "approved"
	case 3:
		return "rejected"
	case 4:
		return "executing"
	case 5:
		return "success"
	case 6:
		return "failed"
	case 7:
		return "cancelled"
	default:
		return fmt.Sprintf("unknown(%d)", status)
	}
}

func checkLevelBadge(level string) string {
	switch strings.ToLower(level) {
	case "error":
		return output.FormatRed(level)
	case "warning", "warn":
		return output.FormatYellow(level)
	case "info":
		return output.FormatBlue(level)
	default:
		return level
	}
}

// formatTime parses common Archery date formats and returns a compact display string.
func formatTime(t string) string {
	if t == "" {
		return ""
	}
	for _, layout := range []string{
		"2006-01-02T15:04:05.000-0700",
		"2006-01-02T15:04:05-07:00",
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02 15:04:05",
		time.RFC3339,
		time.RFC3339Nano,
	} {
		if parsed, err := time.Parse(layout, t); err == nil {
			return parsed.Format("2006-01-02 15:04")
		}
	}
	if len(t) >= 10 {
		return t[:10]
	}
	return t
}
