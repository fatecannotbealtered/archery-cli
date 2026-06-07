package cmd

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/fatecannotbealtered/archery-cli/internal/api"
	"github.com/fatecannotbealtered/archery-cli/internal/output"
	"github.com/spf13/cobra"
)

var workflowCmd = &cobra.Command{
	Use:   "workflow",
	Short: "Manage SQL workflows (submit, audit, execute)",
}

func init() {
	rootCmd.AddCommand(workflowCmd)

	// workflow list
	workflowListCmd.Flags().String("status", "", "Filter by status (e.g. workflow_finish, audit_abort)")
	workflowListCmd.Flags().String("engineer", "", "Filter by engineer/creator username")
	workflowListCmd.Flags().Int("instance", 0, "Filter by instance ID")
	workflowListCmd.Flags().String("db", "", "Filter by database name")
	workflowListCmd.Flags().Int("limit", 20, "Max results per page (1-500)")
	workflowListCmd.Flags().Int("offset", 0, "Pagination offset")
	workflowListCmd.Flags().String("fields", "", "Output only these fields (comma-separated)")
	workflowCmd.AddCommand(workflowListCmd)

	// workflow submit
	workflowSubmitCmd.Flags().String("name", "", "Workflow title (required)")
	workflowSubmitCmd.Flags().Int("instance", 0, "Target instance ID (required)")
	workflowSubmitCmd.Flags().String("db", "", "Target database name (required)")
	workflowSubmitCmd.Flags().String("sql", "", "SQL content (required)")
	workflowSubmitCmd.Flags().Int("group", 0, "Resource group ID")
	workflowSubmitCmd.Flags().Bool("backup", true, "Require backup before execution")
	workflowSubmitCmd.Flags().String("demand-url", "", "Related demand/requirement URL")
	workflowCmd.AddCommand(workflowSubmitCmd)
	markWrite(workflowSubmitCmd)
	markRiskLevel(workflowSubmitCmd, "medium")

	// workflow detail
	workflowDetailCmd.Flags().String("fields", "", "Output only these fields (comma-separated)")
	workflowCmd.AddCommand(workflowDetailCmd)

	// workflow audit
	workflowAuditCmd.Flags().String("action", "", "Audit action: pass or cancel (required)")
	workflowAuditCmd.Flags().String("remark", "", "Audit remark/comment")
	workflowCmd.AddCommand(workflowAuditCmd)
	markWrite(workflowAuditCmd)
	markRiskLevel(workflowAuditCmd, "medium")

	// workflow execute
	workflowExecuteCmd.Flags().String("mode", "auto", "Execution mode: auto or manual")
	workflowCmd.AddCommand(workflowExecuteCmd)
	markWrite(workflowExecuteCmd)
	markRiskLevel(workflowExecuteCmd, "high")

	// workflow cancel
	workflowCancelCmd.Flags().String("remark", "", "Cancellation remark")
	workflowCmd.AddCommand(workflowCancelCmd)
	markWrite(workflowCancelCmd)
	markRiskLevel(workflowCancelCmd, "medium")

	// workflow sqlcheck
	workflowSQLCheckCmd.Flags().Int("instance", 0, "Target instance ID (required)")
	workflowSQLCheckCmd.Flags().String("db", "", "Target database name (required)")
	workflowSQLCheckCmd.Flags().String("sql", "", "SQL to check (required)")
	workflowCmd.AddCommand(workflowSQLCheckCmd)
}

// ─── workflow list ──────────────────────────────────────────────────────────

var workflowListCmd = &cobra.Command{
	Use:   "list",
	Short: "List SQL workflows with optional filters",
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, region, err := newClient()
		if err != nil {
			return err
		}

		limit, _ := cmd.Flags().GetInt("limit")
		if limit < 1 || limit > 500 {
			return failArg("--limit must be between 1 and 500")
		}
		offset, _ := cmd.Flags().GetInt("offset")
		if offset < 0 {
			return failArg("--offset must be >= 0")
		}

		params := api.WorkflowListParams{
			Status:   mustGetString(cmd, "status"),
			Engineer: mustGetString(cmd, "engineer"),
			DBName:   mustGetString(cmd, "db"),
			Limit:    limit,
			Offset:   offset,
		}
		if v, _ := cmd.Flags().GetInt("instance"); v > 0 {
			params.InstanceID = v
		}

		if dryRunOutput("list workflows", map[string]any{"filters": params}) {
			return nil
		}

		page, err := client.Workflows.List(apiCtx(), params)
		if err != nil {
			return handleAPIError(err)
		}

		if jsonMode {
			fields := getFieldsFlag(cmd)
			printWorkflowListJSON(page, fields, region.URL)
			return nil
		}

		if len(page.Results) == 0 {
			output.Info("No workflows found.")
			return nil
		}
		printWorkflowTable(page.Results, region.URL)
		return nil
	},
}

// ─── workflow submit ────────────────────────────────────────────────────────

var workflowSubmitCmd = &cobra.Command{
	Use:   "submit",
	Short: "Submit a new SQL workflow for review",
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, region, err := newClient()
		if err != nil {
			return err
		}

		name, err := requireFlagString(cmd, "name", "--name")
		if err != nil {
			return err
		}
		instanceID, _ := cmd.Flags().GetInt("instance")
		if instanceID == 0 {
			return failArg("--instance is required")
		}
		db, err := requireFlagString(cmd, "db", "--db")
		if err != nil {
			return err
		}
		sql, err := requireFlagString(cmd, "sql", "--sql")
		if err != nil {
			return err
		}
		groupID, _ := cmd.Flags().GetInt("group")
		backup, _ := cmd.Flags().GetBool("backup")
		demandURL, _ := cmd.Flags().GetString("demand-url")

		req := api.WorkflowSubmitRequest{
			Name:       name,
			InstanceID: instanceID,
			DBName:     db,
			SQLContent: sql,
			IsBackup:   backup,
			DemandURL:  demandURL,
		}
		if groupID > 0 {
			req.GroupID = groupID
		}

		detail := map[string]any{
			"name":       name,
			"instanceId": instanceID,
			"db":         db,
			"sql":        sql,
			"backup":     backup,
		}
		if groupID > 0 {
			detail["groupId"] = groupID
		}
		if demandURL != "" {
			detail["demandUrl"] = demandURL
		}
		if dryRunOutput("submit workflow", detail) {
			return nil
		}

		resp, err := client.Workflows.Submit(apiCtx(), req)
		if err != nil {
			return handleAPIError(err)
		}

		url := region.URL + fmt.Sprintf("/sqlworkflow/%d/", resp.ID)
		if jsonMode {
			output.PrintJSON(map[string]any{
				"workflowId": resp.ID,
				"url":        url,
			})
			return nil
		}
		output.Success(fmt.Sprintf("Workflow %d submitted", resp.ID))
		output.Info(url)
		return nil
	},
}

// ─── workflow detail ────────────────────────────────────────────────────────

var workflowDetailCmd = &cobra.Command{
	Use:   "detail <WORKFLOW_ID>",
	Short: "Show workflow details, SQL content, and audit log",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, region, err := newClient()
		if err != nil {
			return err
		}

		id, err := parseWorkflowID(args[0])
		if err != nil {
			return err
		}

		detail, err := client.Workflows.Detail(apiCtx(), id)
		if err != nil {
			return handleAPIError(err)
		}

		if jsonMode {
			fields := getFieldsFlag(cmd)
			printWorkflowDetailJSON(detail, fields, region.URL)
			return nil
		}
		printWorkflowDetail(detail, region.URL)
		return nil
	},
}

// ─── workflow audit ─────────────────────────────────────────────────────────

var workflowAuditCmd = &cobra.Command{
	Use:   "audit <WORKFLOW_ID>",
	Short: "Audit (approve or reject) a workflow",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, _, err := newClient()
		if err != nil {
			return err
		}

		id, err := parseWorkflowID(args[0])
		if err != nil {
			return err
		}

		action, err := requireFlagString(cmd, "action", "--action")
		if err != nil {
			return err
		}
		action = strings.ToLower(action)
		if action != "pass" && action != "cancel" {
			return failArg("--action must be 'pass' or 'cancel'")
		}
		remark, _ := cmd.Flags().GetString("remark")

		req := api.WorkflowAuditRequest{
			WorkflowID: id,
			Action:     action,
			Remark:     remark,
		}

		detail := map[string]any{
			"workflowId": id,
			"action":     action,
		}
		if remark != "" {
			detail["remark"] = remark
		}
		if dryRunOutput("audit workflow", detail) {
			return nil
		}

		if err := client.Workflows.Audit(apiCtx(), req); err != nil {
			return handleAPIError(err)
		}

		if jsonMode {
			output.PrintJSON(map[string]any{
				"workflowId": id,
				"status":     action,
			})
			return nil
		}
		output.Success(fmt.Sprintf("Workflow %d: %s", id, action))
		return nil
	},
}

// ─── workflow execute ───────────────────────────────────────────────────────

var workflowExecuteCmd = &cobra.Command{
	Use:   "execute <WORKFLOW_ID>",
	Short: "Execute an approved workflow",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, _, err := newClient()
		if err != nil {
			return err
		}

		id, err := parseWorkflowID(args[0])
		if err != nil {
			return err
		}

		mode, _ := cmd.Flags().GetString("mode")
		mode = strings.ToLower(mode)
		if mode != "auto" && mode != "manual" {
			return failArg("--mode must be 'auto' or 'manual'")
		}

		req := api.WorkflowExecuteRequest{
			WorkflowID: id,
			Mode:       mode,
		}

		detail := map[string]any{
			"workflowId": id,
			"mode":       mode,
		}
		if dryRunOutput("execute workflow", detail) {
			return nil
		}

		if err := client.Workflows.Execute(apiCtx(), req); err != nil {
			return handleAPIError(err)
		}

		if jsonMode {
			output.PrintJSON(map[string]any{
				"workflowId": id,
				"status":     "executing",
				"mode":       mode,
			})
			return nil
		}
		output.Success(fmt.Sprintf("Workflow %d execution started (%s)", id, mode))
		return nil
	},
}

// ─── workflow cancel ────────────────────────────────────────────────────────

var workflowCancelCmd = &cobra.Command{
	Use:   "cancel <WORKFLOW_ID>",
	Short: "Cancel a running workflow",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, _, err := newClient()
		if err != nil {
			return err
		}

		id, err := parseWorkflowID(args[0])
		if err != nil {
			return err
		}

		remark, _ := cmd.Flags().GetString("remark")

		req := api.WorkflowCancelRequest{
			WorkflowID: id,
			Remark:     remark,
		}

		detail := map[string]any{
			"workflowId": id,
		}
		if remark != "" {
			detail["remark"] = remark
		}
		if dryRunOutput("cancel workflow", detail) {
			return nil
		}

		if err := client.Workflows.Cancel(apiCtx(), req); err != nil {
			return handleAPIError(err)
		}

		if jsonMode {
			output.PrintJSON(map[string]any{
				"workflowId": id,
				"status":     "cancelled",
			})
			return nil
		}
		output.Success(fmt.Sprintf("Workflow %d cancelled", id))
		return nil
	},
}

// ─── workflow sqlcheck ──────────────────────────────────────────────────────

var workflowSQLCheckCmd = &cobra.Command{
	Use:   "sqlcheck",
	Short: "Run SQL syntax and risk checks without submitting a workflow",
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, _, err := newClient()
		if err != nil {
			return err
		}

		instanceID, _ := cmd.Flags().GetInt("instance")
		if instanceID == 0 {
			return failArg("--instance is required")
		}
		db, err := requireFlagString(cmd, "db", "--db")
		if err != nil {
			return err
		}
		sql, err := requireFlagString(cmd, "sql", "--sql")
		if err != nil {
			return err
		}

		req := api.WorkflowSQLCheckRequest{
			InstanceID: instanceID,
			DBName:     db,
			SQLContent: sql,
		}

		results, err := client.Workflows.SQLCheck(apiCtx(), req)
		if err != nil {
			return handleAPIError(err)
		}

		if jsonMode {
			output.PrintJSON(results)
			return nil
		}
		printSQLCheckResults(results)
		return nil
	},
}

// ─── helper ─────────────────────────────────────────────────────────────────

// parseWorkflowID parses and validates a workflow ID string.
func parseWorkflowID(s string) (int, error) {
	id, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || id <= 0 {
		return 0, failArg("WORKFLOW_ID must be a positive integer")
	}
	return id, nil
}

// mustGetString returns the string flag value or empty string.
func mustGetString(cmd *cobra.Command, name string) string {
	v, _ := cmd.Flags().GetString(name)
	return v
}
