package cmd

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/fatecannotbealtered/archery-cli/internal/api"
	"github.com/fatecannotbealtered/archery-cli/internal/config"
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
	workflowListCmd.Flags().String("status", "", "Filter by status code (e.g. workflow_finish, workflow_manreviewing)")
	workflowListCmd.Flags().String("engineer", "", "Filter by engineer/creator username (jwt mode only)")
	workflowListCmd.Flags().Int("instance", 0, "Filter by instance ID")
	workflowListCmd.Flags().Int("group", 0, "Filter by resource group ID (session mode)")
	workflowListCmd.Flags().String("db", "", "Filter by database name (jwt mode only)")
	workflowListCmd.Flags().String("search", "", "Search engineer name or workflow title (session mode)")
	workflowListCmd.Flags().Int("limit", 20, "Max results per page (1-500)")
	workflowListCmd.Flags().Int("offset", 0, "Pagination offset")
	workflowListCmd.Flags().String("fields", "", "Output only these fields (comma-separated)")
	workflowCmd.AddCommand(workflowListCmd)

	// workflow audit-list
	workflowAuditListCmd.Flags().String("status", "", "Filter by status code")
	workflowAuditListCmd.Flags().Int("instance", 0, "Filter by instance ID")
	workflowAuditListCmd.Flags().Int("group", 0, "Filter by resource group ID")
	workflowAuditListCmd.Flags().String("search", "", "Search engineer name or workflow title")
	workflowAuditListCmd.Flags().Int("limit", 20, "Max results per page (1-500)")
	workflowAuditListCmd.Flags().Int("offset", 0, "Pagination offset")
	workflowAuditListCmd.Flags().String("fields", "", "Output only these fields (comma-separated)")
	workflowCmd.AddCommand(workflowAuditListCmd)

	// workflow submit
	workflowSubmitCmd.Flags().String("name", "", "Workflow title (required)")
	workflowSubmitCmd.Flags().Int("instance", 0, "Target instance ID (both modes; session resolves it to a name)")
	workflowSubmitCmd.Flags().String("instance-name", "", "Target instance name (session mode; wins over --instance)")
	workflowSubmitCmd.Flags().String("db", "", "Target database name (required)")
	workflowSubmitCmd.Flags().String("sql", "", "SQL content (required)")
	workflowSubmitCmd.Flags().Int("group", 0, "Resource group ID (both modes; session resolves it to a name)")
	workflowSubmitCmd.Flags().String("group-name", "", "Resource group name (session mode; wins over --group)")
	workflowSubmitCmd.Flags().Bool("backup", true, "Require backup before execution")
	workflowSubmitCmd.Flags().String("demand-url", "", "Related demand/requirement URL")
	workflowSubmitCmd.Flags().StringSlice("cc", nil, "CC notify usernames (session mode)")
	workflowCmd.AddCommand(workflowSubmitCmd)
	markWrite(workflowSubmitCmd)
	markRiskLevel(workflowSubmitCmd, "medium")

	// workflow detail
	workflowDetailCmd.Flags().String("fields", "", "Output only these fields (comma-separated)")
	workflowCmd.AddCommand(workflowDetailCmd)

	// workflow audit
	workflowAuditCmd.Flags().String("action", "", "Audit action: pass or cancel (required)")
	workflowAuditCmd.Flags().String("remark", "", "Audit remark/comment")
	workflowAuditCmd.Flags().StringSlice("ids", nil, "Workflow IDs to audit (comma-separated or repeatable; batch)")
	workflowAuditCmd.Flags().Bool("continue-on-error", true, "Keep auditing after a workflow fails (batch; default true)")
	workflowCmd.AddCommand(workflowAuditCmd)
	markWrite(workflowAuditCmd)
	markRiskLevel(workflowAuditCmd, "medium")

	// workflow execute
	workflowExecuteCmd.Flags().String("mode", "auto", "Execution mode: auto or manual")
	workflowExecuteCmd.Flags().StringSlice("ids", nil, "Workflow IDs to execute (comma-separated or repeatable; batch)")
	// execute is critical: default --continue-on-error false (stop at first failure).
	workflowExecuteCmd.Flags().Bool("continue-on-error", false, "Keep executing after a workflow fails (batch; default false for execute)")
	workflowCmd.AddCommand(workflowExecuteCmd)
	markWrite(workflowExecuteCmd)
	markRiskLevel(workflowExecuteCmd, "high")

	// workflow cancel
	workflowCancelCmd.Flags().String("remark", "", "Cancellation remark")
	workflowCmd.AddCommand(workflowCancelCmd)
	markWrite(workflowCancelCmd)
	markRiskLevel(workflowCancelCmd, "medium")

	// workflow sqlcheck
	workflowSQLCheckCmd.Flags().Int("instance", 0, "Target instance ID (both modes; session resolves it to a name)")
	workflowSQLCheckCmd.Flags().String("instance-name", "", "Target instance name (session mode; wins over --instance)")
	workflowSQLCheckCmd.Flags().String("db", "", "Target database name (required)")
	workflowSQLCheckCmd.Flags().String("sql", "", "SQL to check (required)")
	workflowCmd.AddCommand(workflowSQLCheckCmd)

	// workflow auto-review
	workflowAutoReviewCmd.Flags().Int("instance", 0, "Target instance ID (both modes; session resolves it to a name)")
	workflowAutoReviewCmd.Flags().String("instance-name", "", "Target instance name (session mode; wins over --instance)")
	workflowAutoReviewCmd.Flags().String("db", "", "Target database name (required)")
	workflowAutoReviewCmd.Flags().String("sql", "", "SQL to classify (required)")
	workflowAutoReviewCmd.Flags().StringSlice("ids", nil, "Approve these workflow IDs after they pass the rules (with --execute)")
	workflowAutoReviewCmd.Flags().Bool("execute", false, "Approve (audit pass) compliant workflows; without it, dry-run classify only")
	workflowAutoReviewCmd.Flags().String("remark", "auto-review pass", "Audit remark used when approving with --execute")
	workflowCmd.AddCommand(workflowAutoReviewCmd)
	markWrite(workflowAutoReviewCmd)
	markRiskLevel(workflowAutoReviewCmd, "medium")
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

		params := workflowListParamsFromFlags(cmd, limit, offset)

		result, err := client.Workflows.List(apiCtx(), params)
		if err != nil {
			return handleAPIError(err)
		}

		if jsonMode {
			fields := getFieldsFlag(cmd)
			printWorkflowListJSON(result, fields, region.URL)
			return nil
		}

		if len(result.Page.Results) == 0 {
			output.Info("No workflows found.")
			return nil
		}
		printWorkflowTable(result.Page.Results, region.URL)
		printListPaginationHint(result.Pagination, limit)
		return nil
	},
}

// ─── workflow audit-list ────────────────────────────────────────────────────

var workflowAuditListCmd = &cobra.Command{
	Use:   "audit-list",
	Short: "List workflows pending the current user's audit",
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

		params := workflowListParamsFromFlags(cmd, limit, offset)
		params.Audit = true

		result, err := client.Workflows.List(apiCtx(), params)
		if err != nil {
			return handleAPIError(err)
		}

		if jsonMode {
			fields := getFieldsFlag(cmd)
			printWorkflowListJSON(result, fields, region.URL)
			return nil
		}

		if len(result.Page.Results) == 0 {
			output.Info("No workflows pending your audit.")
			return nil
		}
		printWorkflowTable(result.Page.Results, region.URL)
		printListPaginationHint(result.Pagination, limit)
		return nil
	},
}

// workflowListParamsFromFlags builds the list params shared by `list` and
// `audit-list`. The engineer/db filters apply only to the JWT REST endpoint;
// the session endpoint uses navStatus/instance_id/group_id/search.
func workflowListParamsFromFlags(cmd *cobra.Command, limit, offset int) api.WorkflowListParams {
	params := api.WorkflowListParams{
		Status: mustGetString(cmd, "status"),
		Search: mustGetString(cmd, "search"),
		Limit:  limit,
		Offset: offset,
	}
	if cmd.Flags().Lookup("engineer") != nil {
		params.Engineer = mustGetString(cmd, "engineer")
	}
	if cmd.Flags().Lookup("db") != nil {
		params.DBName = mustGetString(cmd, "db")
	}
	if v, _ := cmd.Flags().GetInt("instance"); v > 0 {
		params.InstanceID = v
	}
	if v, _ := cmd.Flags().GetInt("group"); v > 0 {
		params.GroupID = v
	}
	return params
}

// ─── workflow submit ────────────────────────────────────────────────────────

var workflowSubmitCmd = &cobra.Command{
	Use:   "submit",
	Short: "Submit a new SQL workflow for review",
	RunE: func(cmd *cobra.Command, args []string) error {
		name, err := requireFlagString(cmd, "name", "--name")
		if err != nil {
			return err
		}
		db, err := requireFlagString(cmd, "db", "--db")
		if err != nil {
			return err
		}
		sql, err := requireFlagString(cmd, "sql", "--sql")
		if err != nil {
			return err
		}
		instanceID, _ := cmd.Flags().GetInt("instance")
		instanceName, _ := cmd.Flags().GetString("instance-name")
		groupID, _ := cmd.Flags().GetInt("group")
		groupName, _ := cmd.Flags().GetString("group-name")
		backup, _ := cmd.Flags().GetBool("backup")
		demandURL, _ := cmd.Flags().GetString("demand-url")
		ccUsers, _ := cmd.Flags().GetStringSlice("cc")

		getClientRegion := newClientRegionMemo()
		getClient := func() (*api.Client, error) {
			c, _, err := getClientRegion()
			return c, err
		}
		mode := activeTransportMode()
		// Session submit keys on names; REST keys on numeric IDs. In session mode
		// the agent may pass the name directly, or the numeric ID (resolved to a
		// name here) — same flags as JWT mode, no per-transport memorisation. The
		// name flag wins when both are present.
		if mode == api.ModeSession {
			if instanceName == "" {
				if instanceID == 0 {
					return failArg("--instance-name or --instance (ID) is required in session mode")
				}
				instanceName, err = resolveInstanceName(getClient, instanceID)
				if err != nil {
					return err
				}
			}
			if groupName == "" {
				if groupID == 0 {
					return failArg("--group-name or --group (ID) is required in session mode")
				}
				groupName, err = resolveGroupName(getClient, groupID)
				if err != nil {
					return err
				}
			}
			// The session endpoint keys on names; clear the IDs so the request and
			// preview never carry a stale numeric identifier the server ignores.
			instanceID, groupID = 0, 0
		} else if instanceID == 0 {
			return failArg("--instance is required in jwt mode")
		}

		req := api.WorkflowSubmitRequest{
			Name:         name,
			InstanceID:   instanceID,
			InstanceName: instanceName,
			DBName:       db,
			SQLContent:   sql,
			GroupID:      groupID,
			GroupName:    groupName,
			IsBackup:     backup,
			DemandURL:    demandURL,
			CCUsers:      ccUsers,
		}

		detail := map[string]any{
			"name":   name,
			"db":     db,
			"sql":    sql,
			"backup": backup,
			"mode":   mode,
		}
		if instanceName != "" {
			detail["instanceName"] = instanceName
		}
		if instanceID > 0 {
			detail["instanceId"] = strconv.Itoa(instanceID)
		}
		if groupName != "" {
			detail["groupName"] = groupName
		}
		if groupID > 0 {
			detail["groupId"] = strconv.Itoa(groupID)
		}
		if demandURL != "" {
			detail["demandUrl"] = demandURL
		}
		if markDryRunOrConfirm("submit workflow", detail) {
			return nil
		}

		client, region, err := getClientRegion()
		if err != nil {
			return err
		}

		resp, err := client.Workflows.Submit(apiCtx(), req)
		if err != nil {
			return handleAPIError(err)
		}

		url := region.URL + fmt.Sprintf("/sqlworkflow/%d/", resp.ID)
		if jsonMode {
			output.PrintJSON(map[string]any{
				"workflowId": strconv.Itoa(resp.ID),
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
	Use:   "audit [WORKFLOW_ID]",
	Short: "Audit (approve or reject) one workflow, or a batch via --ids",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		action, err := requireFlagString(cmd, "action", "--action")
		if err != nil {
			return err
		}
		action = strings.ToLower(action)
		if action != "pass" && action != "cancel" {
			return failArg("--action must be 'pass' or 'cancel'")
		}
		remark, _ := cmd.Flags().GetString("remark")

		ids, err := resolveWorkflowTargets(cmd, args)
		if err != nil {
			return err
		}
		if len(ids) > 1 || cmd.Flags().Changed("ids") {
			return runWorkflowAuditBatch(cmd, ids, action, remark)
		}
		id := ids[0]

		req := api.WorkflowAuditRequest{
			WorkflowID: id,
			Action:     action,
			Remark:     remark,
		}

		detail := map[string]any{
			"workflowId": strconv.Itoa(id),
			"action":     action,
		}
		if remark != "" {
			detail["remark"] = remark
		}
		if markDryRunOrConfirm("audit workflow", detail) {
			return nil
		}

		client, _, _, err := newClient()
		if err != nil {
			return err
		}

		if err := client.Workflows.Audit(apiCtx(), req); err != nil {
			return handleAPIError(err)
		}

		if jsonMode {
			output.PrintJSON(map[string]any{
				"workflowId": strconv.Itoa(id),
				"status":     action,
			})
			return nil
		}
		output.Success(fmt.Sprintf("Workflow %d: %s", id, action))
		return nil
	},
}

// runWorkflowAuditBatch audits many workflows in one batch (CLI-SPEC §15). audit
// is not irreversible, so the whole batch shares one confirm token (no per-item
// confirm) and defaults to --continue-on-error true. Client-side loop; not atomic.
func runWorkflowAuditBatch(cmd *cobra.Command, ids []int, action, remark string) error {
	targets := make([]string, len(ids))
	changes := make([]map[string]any, len(ids))
	for i, id := range ids {
		targets[i] = strconv.Itoa(id)
		changes[i] = map[string]any{"action": "audit:" + action, "workflowId": strconv.Itoa(id)}
	}
	if batchDryRunOrConfirm("audit workflows", targets, changes) {
		return nil
	}

	client, _, _, err := newClient()
	if err != nil {
		return err
	}

	continueOnError := continueOnErrorFlag(cmd, true)
	items, summary := runBatch(targets, continueOnError, func(target string) (map[string]any, output.ErrorCode, bool, error) {
		id, _ := strconv.Atoi(target)
		req := api.WorkflowAuditRequest{WorkflowID: id, Action: action, Remark: remark}
		if err := client.Workflows.Audit(apiCtx(), req); err != nil {
			code := errorCodeForAPIErr(err)
			return nil, code, output.RetryableErrorCode(code), err
		}
		return map[string]any{"status": action}, "", false, nil
	})

	printBatchResult(items, summary)
	return nil
}

// ─── workflow execute ───────────────────────────────────────────────────────

var workflowExecuteCmd = &cobra.Command{
	Use:   "execute [WORKFLOW_ID]",
	Short: "Execute one approved workflow, or a batch via --ids",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		mode, _ := cmd.Flags().GetString("mode")
		mode = strings.ToLower(mode)
		if mode != "auto" && mode != "manual" {
			return failArg("--mode must be 'auto' or 'manual'")
		}

		ids, err := resolveWorkflowTargets(cmd, args)
		if err != nil {
			return err
		}
		if len(ids) > 1 || cmd.Flags().Changed("ids") {
			return runWorkflowExecuteBatch(cmd, ids, mode)
		}
		id := ids[0]

		detail := map[string]any{
			"workflowId": strconv.Itoa(id),
			"mode":       mode,
		}
		if markDryRunOrConfirm("execute workflow", detail) {
			return nil
		}

		client, _, region, err := newClient()
		if err != nil {
			return err
		}

		req := api.WorkflowExecuteRequest{
			WorkflowID:   id,
			WorkflowType: api.WorkflowTypeSQLReview,
			Engineer:     region.Username,
			Mode:         mode,
		}

		if err := client.Workflows.Execute(apiCtx(), req); err != nil {
			return handleAPIError(err)
		}

		if jsonMode {
			output.PrintJSON(map[string]any{
				"workflowId": strconv.Itoa(id),
				"status":     "executing",
				"mode":       mode,
			})
			return nil
		}
		output.Success(fmt.Sprintf("Workflow %d execution started (%s)", id, mode))
		return nil
	},
}

// runWorkflowExecuteBatch executes many approved workflows in one batch. execute
// is critical and irreversible, so it is more conservative than the generic
// contract (CLI-SPEC §15.4): the --dangerous gate is required (enforced by the
// high risk level), and --continue-on-error defaults to false so the batch stops
// at the first failure rather than blasting through a bad release window.
// Already-executed workflows stay executed (no rollback); the unattempted
// remainder is reported as skipped so the agent can resume.
func runWorkflowExecuteBatch(cmd *cobra.Command, ids []int, mode string) error {
	targets := make([]string, len(ids))
	changes := make([]map[string]any, len(ids))
	for i, id := range ids {
		targets[i] = strconv.Itoa(id)
		changes[i] = map[string]any{"action": "execute", "workflowId": strconv.Itoa(id), "mode": mode}
	}
	if batchDryRunOrConfirm("execute workflows", targets, changes) {
		return nil
	}

	client, _, region, err := newClient()
	if err != nil {
		return err
	}

	continueOnError := continueOnErrorFlag(cmd, false)
	items, summary := runBatch(targets, continueOnError, func(target string) (map[string]any, output.ErrorCode, bool, error) {
		id, _ := strconv.Atoi(target)
		req := api.WorkflowExecuteRequest{
			WorkflowID:   id,
			WorkflowType: api.WorkflowTypeSQLReview,
			Engineer:     region.Username,
			Mode:         mode,
		}
		if err := client.Workflows.Execute(apiCtx(), req); err != nil {
			code := errorCodeForAPIErr(err)
			return nil, code, output.RetryableErrorCode(code), err
		}
		return map[string]any{"status": "executing", "mode": mode}, "", false, nil
	})

	printBatchResult(items, summary)
	return nil
}

// ─── workflow cancel ────────────────────────────────────────────────────────

var workflowCancelCmd = &cobra.Command{
	Use:   "cancel <WORKFLOW_ID>",
	Short: "Cancel a running workflow",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
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
			"workflowId": strconv.Itoa(id),
		}
		if remark != "" {
			detail["remark"] = remark
		}
		if markDryRunOrConfirm("cancel workflow", detail) {
			return nil
		}

		client, _, _, err := newClient()
		if err != nil {
			return err
		}

		if err := client.Workflows.Cancel(apiCtx(), req); err != nil {
			return handleAPIError(err)
		}

		if jsonMode {
			output.PrintJSON(map[string]any{
				"workflowId": strconv.Itoa(id),
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
		db, err := requireFlagString(cmd, "db", "--db")
		if err != nil {
			return err
		}
		sql, err := requireFlagString(cmd, "sql", "--sql")
		if err != nil {
			return err
		}

		getClient := newClientMemo()
		instanceID, instanceName, err := workflowInstanceIdentifier(cmd, getClient)
		if err != nil {
			return err
		}

		client, err := getClient()
		if err != nil {
			return err
		}

		req := api.WorkflowSQLCheckRequest{
			InstanceID:   instanceID,
			InstanceName: instanceName,
			DBName:       db,
			SQLContent:   sql,
		}

		results, err := client.Workflows.SQLCheck(apiCtx(), req)
		if err != nil {
			return handleAPIError(err)
		}

		if jsonMode {
			items := make([]map[string]any, len(results))
			for i, r := range results {
				items[i] = sqlCheckResultToMap(r)
			}
			output.PrintJSON(items)
			return nil
		}
		printSQLCheckResults(results)
		return nil
	},
}

// ─── workflow auto-review ───────────────────────────────────────────────────

var workflowAutoReviewCmd = &cobra.Command{
	Use:   "auto-review",
	Short: "Classify SQL by the auto-review rules; optionally approve compliant workflows",
	Long: "Runs the SQL pre-check (/simplecheck/) and classifies each statement as " +
		"pass or block by the auto-review rules: a statement blocks if its errlevel " +
		"is non-zero (a syntax/risk error). Without --execute this is a read-only " +
		"dry-run that reports the classification. With --execute it approves (audit " +
		"pass) the workflow IDs given in --ids — which needs auditor permission.",
	RunE: func(cmd *cobra.Command, args []string) error {
		db, err := requireFlagString(cmd, "db", "--db")
		if err != nil {
			return err
		}
		sql, err := requireFlagString(cmd, "sql", "--sql")
		if err != nil {
			return err
		}
		getClient := newClientMemo()
		instanceID, instanceName, err := workflowInstanceIdentifier(cmd, getClient)
		if err != nil {
			return err
		}
		execute, _ := cmd.Flags().GetBool("execute")
		remark, _ := cmd.Flags().GetString("remark")
		ids, _ := cmd.Flags().GetStringSlice("ids")
		approveIDs := parsePluralTargets(ids)
		if execute && len(approveIDs) == 0 {
			return failArg("--execute needs --ids listing the workflow IDs to approve")
		}

		client, err := getClient()
		if err != nil {
			return err
		}

		// Classify: run the pre-check and bucket statements by the rules.
		results, err := client.Workflows.SQLCheck(apiCtx(), api.WorkflowSQLCheckRequest{
			InstanceID:   instanceID,
			InstanceName: instanceName,
			DBName:       db,
			SQLContent:   sql,
		})
		if err != nil {
			return handleAPIError(err)
		}

		blocked := 0
		classified := make([]map[string]any, len(results))
		for i, r := range results {
			verdict := "pass"
			// errlevel: 0 ok, 1 warning, 2 error. Any non-zero level blocks.
			if r.ErrLevel != 0 {
				verdict = "block"
				blocked++
			}
			classified[i] = normalizeAgentMap(map[string]any{
				"sql":          r.SQL,
				"errlevel":     r.ErrLevel,
				"level":        r.Level,
				"stagestatus":  r.StageStatus,
				"message":      r.Message,
				"affectedRows": r.AffectedRows,
				"verdict":      verdict,
			})
		}
		compliant := blocked == 0

		// Without --execute (or when blocked), report the classification only.
		if !execute {
			return printAutoReviewResult(classified, compliant, blocked, nil)
		}
		if !compliant {
			output.Warn("auto-review found blocking statements; not approving any workflow.")
			return printAutoReviewResult(classified, compliant, blocked, nil)
		}

		// --execute: approve the listed workflows. Approval is reversible (cancel),
		// so the batch shares one confirm token (CLI-SPEC §15).
		targets := make([]string, len(approveIDs))
		changes := make([]map[string]any, len(approveIDs))
		for i, t := range approveIDs {
			targets[i] = t
			changes[i] = map[string]any{"action": "audit:pass", "workflowId": t}
		}
		if batchDryRunOrConfirm("auto-review approve workflows", targets, changes) {
			return nil
		}

		items, summary := runBatch(targets, true, func(target string) (map[string]any, output.ErrorCode, bool, error) {
			id, perr := strconv.Atoi(target)
			if perr != nil || id <= 0 {
				return nil, output.E_VALIDATION, false, fmt.Errorf("invalid workflow id %q", target)
			}
			req := api.WorkflowAuditRequest{WorkflowID: id, Action: "pass", Remark: remark}
			if aerr := client.Workflows.Audit(apiCtx(), req); aerr != nil {
				code := errorCodeForAPIErr(aerr)
				return nil, code, output.RetryableErrorCode(code), aerr
			}
			return map[string]any{"status": "pass"}, "", false, nil
		})
		printBatchResult(items, summary)
		return nil
	},
}

// printAutoReviewResult renders the auto-review classification. In JSON mode it
// emits {compliant,blocked,results[]}; otherwise a per-statement table.
func printAutoReviewResult(classified []map[string]any, compliant bool, blocked int, _ any) error {
	if jsonMode {
		output.PrintJSON(map[string]any{
			"compliant": compliant,
			"blocked":   blocked,
			"results":   classified,
		})
		return nil
	}
	headers := []string{"VERDICT", "LEVEL", "MESSAGE", "SQL"}
	rows := make([][]string, len(classified))
	for i, c := range classified {
		sqlText, _ := c["sql"].(string)
		if len(sqlText) > 60 {
			sqlText = sqlText[:57] + "..."
		}
		msg, _ := c["message"].(string)
		level, _ := c["level"].(string)
		verdict, _ := c["verdict"].(string)
		rows[i] = []string{verdict, level, msg, sqlText}
	}
	output.Table(headers, rows)
	if compliant {
		output.Success("auto-review: all statements pass the rules.")
	} else {
		output.Warn(fmt.Sprintf("auto-review: %d statement(s) blocked.", blocked))
	}
	return nil
}

// ─── helper ─────────────────────────────────────────────────────────────────

// workflowInstanceIdentifier resolves the instance identifier the active
// transport needs: session mode keys on --instance-name, JWT mode on the
// numeric --instance. Returns (id, name) with the unused side zero/empty.
//
// In session mode the caller may pass --instance-name directly, or supply the
// numeric --instance (ID) which is resolved to a name via the instance list —
// so agents no longer have to remember a different flag per transport. The name
// flag wins when both are given. lazyClient builds the API client on first use
// (only when an ID actually needs resolving), so the no-resolution paths stay
// network-free for the dry-run gate.
func workflowInstanceIdentifier(cmd *cobra.Command, lazyClient func() (*api.Client, error)) (int, string, error) {
	instanceID, _ := cmd.Flags().GetInt("instance")
	instanceName, _ := cmd.Flags().GetString("instance-name")
	if activeTransportMode() == api.ModeSession {
		if instanceName != "" {
			return 0, instanceName, nil
		}
		if instanceID > 0 {
			name, err := resolveInstanceName(lazyClient, instanceID)
			if err != nil {
				return 0, "", err
			}
			return 0, name, nil
		}
		return 0, "", failArg("--instance-name or --instance (ID) is required in session mode")
	}
	if instanceID == 0 {
		return 0, "", failArg("--instance is required in jwt mode")
	}
	return instanceID, "", nil
}

// resolveInstanceName maps a numeric instance ID to its instance name by paging
// the instance list (session mode has no by-id JSON endpoint). A page of 500
// covers any realistic single-region fleet.
func resolveInstanceName(lazyClient func() (*api.Client, error), instanceID int) (string, error) {
	client, err := lazyClient()
	if err != nil {
		return "", err
	}
	instances, _, err := listInstancesSession(client, "", "", "", 500, 0)
	if err != nil {
		return "", handleAPIError(err)
	}
	want := strconv.Itoa(instanceID)
	for _, inst := range instances {
		if inst.ID == want {
			return inst.InstanceName, nil
		}
	}
	return "", failNotFound(fmt.Sprintf("instance ID %d not found; check 'archery-cli instance list'", instanceID))
}

// resolveGroupName maps a numeric resource-group ID to its group name via the
// resource-group list. Used so session-mode submit accepts --group (ID) too.
func resolveGroupName(lazyClient func() (*api.Client, error), groupID int) (string, error) {
	client, err := lazyClient()
	if err != nil {
		return "", err
	}
	groups, _, err := client.Users.ListResourceGroups(apiCtx(), 500, 0, "")
	if err != nil {
		return "", handleAPIError(err)
	}
	for _, g := range groups {
		if g.ID == groupID {
			return g.Name, nil
		}
	}
	return "", failNotFound(fmt.Sprintf("resource group ID %d not found; check 'archery-cli user resource-groups'", groupID))
}

// newClientMemo returns a function that builds the API client once and caches
// it (and any error). Commands that may need the client both for ID→name
// resolution and for the actual request use this so newClient runs at most once.
func newClientMemo() func() (*api.Client, error) {
	get := newClientRegionMemo()
	return func() (*api.Client, error) {
		c, _, err := get()
		return c, err
	}
}

// newClientRegionMemo is newClientMemo but also exposes the active region (for
// commands that need region.URL after the request, e.g. submit).
func newClientRegionMemo() func() (*api.Client, *config.RegionConfig, error) {
	var (
		client *api.Client
		region *config.RegionConfig
		cached bool
		cerr   error
	)
	return func() (*api.Client, *config.RegionConfig, error) {
		if cached {
			return client, region, cerr
		}
		cached = true
		client, _, region, cerr = newClient()
		return client, region, cerr
	}
}

// parseWorkflowID parses and validates a workflow ID string.
func parseWorkflowID(s string) (int, error) {
	id, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || id <= 0 {
		return 0, failArg("WORKFLOW_ID must be a positive integer")
	}
	return id, nil
}

// resolveWorkflowTargets returns the workflow IDs to act on, accepting either a
// single positional arg or the plural --ids flag (comma-separated/repeatable),
// but not both. IDs are de-duplicated in input order (CLI-SPEC §15.1). An empty
// target set is a usage error.
func resolveWorkflowTargets(cmd *cobra.Command, args []string) ([]int, error) {
	plural, _ := cmd.Flags().GetStringSlice("ids")
	pluralSet := cmd.Flags().Changed("ids")

	if pluralSet && len(args) > 0 {
		return nil, failArg("pass either a WORKFLOW_ID argument or --ids, not both")
	}
	if !pluralSet {
		if len(args) == 0 {
			return nil, failArg("a WORKFLOW_ID argument or --ids is required")
		}
		id, err := parseWorkflowID(args[0])
		if err != nil {
			return nil, err
		}
		return []int{id}, nil
	}

	targets := parsePluralTargets(plural)
	if len(targets) == 0 {
		return nil, failArg("--ids must list at least one workflow ID")
	}
	ids := make([]int, 0, len(targets))
	for _, t := range targets {
		id, err := parseWorkflowID(t)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// mustGetString returns the string flag value or empty string.
func mustGetString(cmd *cobra.Command, name string) string {
	v, _ := cmd.Flags().GetString(name)
	return v
}

func sqlCheckResultToMap(r api.SQLCheckResult) map[string]any {
	m := map[string]any{
		"level":         r.Level,
		"message":       r.Message,
		"affected_rows": r.AffectedRows,
		"sql":           r.SQL,
		"errlevel":      r.ErrLevel,
	}
	if r.StageStatus != "" {
		m["stagestatus"] = r.StageStatus
	}
	return normalizeAgentMap(m)
}
