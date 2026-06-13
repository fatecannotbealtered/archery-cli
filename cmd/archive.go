package cmd

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/fatecannotbealtered/archery-cli/internal/output"
	"github.com/spf13/cobra"
)

var archiveCmd = &cobra.Command{
	Use:   "archive",
	Short: "Manage data archiving tasks",
}

func init() {
	rootCmd.AddCommand(archiveCmd)

	// archive list
	archiveListCmd.Flags().Int("instance", 0, "Filter by instance ID")
	archiveListCmd.Flags().String("state", "", "Filter by state: enable|disable")
	archiveListCmd.Flags().Int("limit", 20, "Max results (1-500)")
	archiveListCmd.Flags().Int("offset", 0, "Pagination offset")
	archiveListCmd.Flags().String("search", "", "Search by title")
	archiveListCmd.Flags().String("fields", "", "Comma-separated fields for JSON output")
	archiveCmd.AddCommand(archiveListCmd)

	// archive apply
	archiveApplyCmd.Flags().String("title", "", "Archive task title (required)")
	archiveApplyCmd.Flags().String("group", "", "Resource group name (required)")
	archiveApplyCmd.Flags().String("src-instance", "", "Source instance name (required)")
	archiveApplyCmd.Flags().String("src-db", "", "Source database name (required)")
	archiveApplyCmd.Flags().String("src-table", "", "Source table name (required)")
	archiveApplyCmd.Flags().String("mode", "", "Archive mode: dest|file|purge (required)")
	archiveApplyCmd.Flags().String("dest-instance", "", "Destination instance name (mode=dest)")
	archiveApplyCmd.Flags().String("dest-db", "", "Destination database name (mode=dest)")
	archiveApplyCmd.Flags().String("dest-table", "", "Destination table name (mode=dest)")
	archiveApplyCmd.Flags().String("condition", "", "Archive condition (WHERE clause)")
	archiveApplyCmd.Flags().Bool("no-delete", false, "Do not delete source rows after archiving")
	archiveApplyCmd.Flags().Int("sleep", 0, "Sleep seconds between batch operations")
	archiveCmd.AddCommand(archiveApplyCmd)
	markWrite(archiveApplyCmd)
	markConfirm(archiveApplyCmd)
	markRiskLevel(archiveApplyCmd, "high")

	// archive audit
	archiveAuditCmd.Flags().String("action", "", "Audit action: pass|cancel (required)")
	archiveAuditCmd.Flags().String("remark", "", "Audit remark/comment")
	archiveCmd.AddCommand(archiveAuditCmd)
	markWrite(archiveAuditCmd)
	markRiskLevel(archiveAuditCmd, "medium")

	// archive switch
	archiveSwitchCmd.Flags().String("state", "", "Target state: enable|disable (required)")
	archiveCmd.AddCommand(archiveSwitchCmd)
	markWrite(archiveSwitchCmd)
	markRiskLevel(archiveSwitchCmd, "medium")

	// archive once
	archiveOnceCmd.Flags().String("fields", "", "Comma-separated fields for JSON output")
	archiveCmd.AddCommand(archiveOnceCmd)
	markWrite(archiveOnceCmd)
	markRiskLevel(archiveOnceCmd, "high")

	// archive log
	archiveLogCmd.Flags().Int("limit", 20, "Max results (1-500)")
	archiveLogCmd.Flags().Int("offset", 0, "Pagination offset")
	archiveLogCmd.Flags().String("fields", "", "Comma-separated fields for JSON output")
	archiveCmd.AddCommand(archiveLogCmd)
}

// ─── archive list ──────────────────────────────────────────────────────────

var archiveListCmd = &cobra.Command{
	Use:   "list",
	Short: "List archive tasks",
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, _, err := newClient()
		if err != nil {
			return err
		}

		instanceID, _ := cmd.Flags().GetInt("instance")
		state, _ := cmd.Flags().GetString("state")
		limit, _ := cmd.Flags().GetInt("limit")
		offset, _ := cmd.Flags().GetInt("offset")
		search, _ := cmd.Flags().GetString("search")

		if limit < 1 || limit > 500 {
			return failArg("--limit must be between 1 and 500")
		}
		if offset < 0 {
			return failArg("--offset must be >= 0")
		}
		if state != "" {
			state = strings.ToLower(state)
			if state != "enable" && state != "disable" {
				return failArg("--state must be 'enable' or 'disable'")
			}
		}

		params := url.Values{}
		if instanceID > 0 {
			params.Set("instance_id", strconv.Itoa(instanceID))
		}
		if state != "" {
			params.Set("state", state)
		}
		if search != "" {
			params.Set("search", search)
		}
		params.Set("limit", strconv.Itoa(limit))
		params.Set("offset", strconv.Itoa(offset))

		path := "/archive/list/"
		if encoded := params.Encode(); encoded != "" {
			path += "?" + encoded
		}

		data, err := client.InternalGet(apiCtx(), path)
		if err != nil {
			return handleAPIError(err)
		}

		if jsonMode {
			fields := getFieldsFlag(cmd)
			var raw any
			if err := json.Unmarshal(data, &raw); err != nil {
				return failWithCode("parsing response: "+err.Error(), ExitNetwork, output.E_SERVER)
			}
			items := extractArchiveItems(raw)
			total := extractArchiveTotal(raw)
			if len(fields) > 0 {
				filtered := make([]map[string]any, len(items))
				for i, m := range items {
					filtered[i] = output.FilterMap(m, fields)
				}
				items = filtered
			}
			output.PrintJSON(offsetListEnvelope(items, offset, len(items), total))
			return nil
		}

		// Text mode
		var resp struct {
			Status int    `json:"status"`
			Msg    string `json:"msg"`
			Data   struct {
				Total int              `json:"total"`
				Rows  []map[string]any `json:"rows"`
			} `json:"data"`
		}
		if err := json.Unmarshal(data, &resp); err != nil {
			return failArg("failed to parse response: " + err.Error())
		}
		if resp.Status != 0 && resp.Msg != "" {
			return failArg(resp.Msg)
		}
		if len(resp.Data.Rows) == 0 {
			output.Info("No archive tasks found.")
			return nil
		}

		headers := []string{"ID", "TITLE", "STATE", "INSTANCE", "SRC_DB", "SRC_TABLE", "MODE"}
		rows := make([][]string, len(resp.Data.Rows))
		for i, r := range resp.Data.Rows {
			rows[i] = []string{
				fmt.Sprintf("%v", r["id"]),
				fmt.Sprintf("%v", r["title"]),
				fmt.Sprintf("%v", r["state"]),
				fmt.Sprintf("%v", r["src_instance"]),
				fmt.Sprintf("%v", r["src_db"]),
				fmt.Sprintf("%v", r["src_table"]),
				fmt.Sprintf("%v", r["mode"]),
			}
		}
		output.Table(headers, rows)
		return nil
	},
}

// ─── archive apply ─────────────────────────────────────────────────────────

var archiveApplyCmd = &cobra.Command{
	Use:   "apply",
	Short: "Apply for a new archive task",
	RunE: func(cmd *cobra.Command, args []string) error {
		title, err := requireFlagString(cmd, "title", "--title")
		if err != nil {
			return err
		}
		group, err := requireFlagString(cmd, "group", "--group")
		if err != nil {
			return err
		}
		srcInstance, err := requireFlagString(cmd, "src-instance", "--src-instance")
		if err != nil {
			return err
		}
		srcDB, err := requireFlagString(cmd, "src-db", "--src-db")
		if err != nil {
			return err
		}
		srcTable, err := requireFlagString(cmd, "src-table", "--src-table")
		if err != nil {
			return err
		}
		mode, err := requireFlagString(cmd, "mode", "--mode")
		if err != nil {
			return err
		}
		mode = strings.ToLower(mode)
		if mode != "dest" && mode != "file" && mode != "purge" {
			return failArg("--mode must be one of: dest, file, purge")
		}

		destInstance, _ := cmd.Flags().GetString("dest-instance")
		destDB, _ := cmd.Flags().GetString("dest-db")
		destTable, _ := cmd.Flags().GetString("dest-table")
		condition, _ := cmd.Flags().GetString("condition")
		noDelete, _ := cmd.Flags().GetBool("no-delete")
		sleep, _ := cmd.Flags().GetInt("sleep")

		if mode == "dest" {
			if destInstance == "" || destDB == "" || destTable == "" {
				return failArg("--dest-instance, --dest-db, and --dest-table are required when --mode=dest")
			}
		}

		form := url.Values{
			"title":        {title},
			"group_name":   {group},
			"src_instance": {srcInstance},
			"src_db":       {srcDB},
			"src_table":    {srcTable},
			"mode":         {mode},
		}
		if mode == "dest" {
			form.Set("dest_instance", destInstance)
			form.Set("dest_db", destDB)
			form.Set("dest_table", destTable)
		}
		if condition != "" {
			form.Set("condition", condition)
		}
		if noDelete {
			form.Set("no_delete", "true")
		}
		if sleep > 0 {
			form.Set("sleep", strconv.Itoa(sleep))
		}

		detail := map[string]any{
			"title":       title,
			"group":       group,
			"srcInstance": srcInstance,
			"srcDb":       srcDB,
			"srcTable":    srcTable,
			"mode":        mode,
		}
		if destInstance != "" {
			detail["destInstance"] = destInstance
		}
		if destDB != "" {
			detail["destDb"] = destDB
		}
		if destTable != "" {
			detail["destTable"] = destTable
		}
		if condition != "" {
			detail["condition"] = condition
		}
		if noDelete {
			detail["noDelete"] = true
		}
		if sleep > 0 {
			detail["sleep"] = sleep
		}
		if markDryRunOrConfirm("apply archive task", detail) {
			return nil
		}

		client, _, _, err := newClient()
		if err != nil {
			return err
		}

		data, err := client.InternalPost(apiCtx(), "/archive/apply/", form)
		if err != nil {
			return handleAPIError(err)
		}

		if jsonMode {
			var raw any
			if err := json.Unmarshal(data, &raw); err != nil {
				return failWithCode("parsing response: "+err.Error(), ExitNetwork, output.E_SERVER)
			}
			output.PrintJSON(normalizeAgentValue(raw))
			return nil
		}

		var resp struct {
			Status int    `json:"status"`
			Msg    string `json:"msg"`
		}
		if err := json.Unmarshal(data, &resp); err != nil {
			return failArg("failed to parse response: " + err.Error())
		}
		if resp.Status != 0 && resp.Msg != "" {
			return failArg(resp.Msg)
		}
		output.Success("Archive task applied successfully")
		return nil
	},
}

// ─── archive audit ─────────────────────────────────────────────────────────

var archiveAuditCmd = &cobra.Command{
	Use:   "audit <ARCHIVE_ID>",
	Short: "Audit (approve or reject) an archive task",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := parseArchiveID(args[0])
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

		form := url.Values{
			"archive_id": {strconv.Itoa(id)},
			"action":     {action},
		}
		if remark != "" {
			form.Set("remark", remark)
		}

		detail := map[string]any{
			"archiveId": strconv.Itoa(id),
			"action":    action,
		}
		if markDryRunOrConfirm("audit archive task", detail) {
			return nil
		}

		client, _, _, err := newClient()
		if err != nil {
			return err
		}

		data, err := client.InternalPost(apiCtx(), "/archive/audit/", form)
		if err != nil {
			return handleAPIError(err)
		}

		if jsonMode {
			var raw any
			if err := json.Unmarshal(data, &raw); err != nil {
				return failWithCode("parsing response: "+err.Error(), ExitNetwork, output.E_SERVER)
			}
			output.PrintJSON(normalizeAgentValue(raw))
			return nil
		}

		var resp struct {
			Status int    `json:"status"`
			Msg    string `json:"msg"`
		}
		if err := json.Unmarshal(data, &resp); err != nil {
			return failArg("failed to parse response: " + err.Error())
		}
		if resp.Status != 0 && resp.Msg != "" {
			return failArg(resp.Msg)
		}
		output.Success(fmt.Sprintf("Archive task %d: %s", id, action))
		return nil
	},
}

// ─── archive switch ────────────────────────────────────────────────────────

var archiveSwitchCmd = &cobra.Command{
	Use:   "switch <ARCHIVE_ID>",
	Short: "Enable or disable an archive task",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := parseArchiveID(args[0])
		if err != nil {
			return err
		}

		state, err := requireFlagString(cmd, "state", "--state")
		if err != nil {
			return err
		}
		state = strings.ToLower(state)
		if state != "enable" && state != "disable" {
			return failArg("--state must be 'enable' or 'disable'")
		}

		form := url.Values{
			"archive_id": {strconv.Itoa(id)},
			"state":      {state},
		}

		detail := map[string]any{
			"archiveId": strconv.Itoa(id),
			"state":     state,
		}
		if markDryRunOrConfirm("switch archive state", detail) {
			return nil
		}

		client, _, _, err := newClient()
		if err != nil {
			return err
		}

		data, err := client.InternalPost(apiCtx(), "/archive/switch/", form)
		if err != nil {
			return handleAPIError(err)
		}

		if jsonMode {
			var raw any
			if err := json.Unmarshal(data, &raw); err != nil {
				return failWithCode("parsing response: "+err.Error(), ExitNetwork, output.E_SERVER)
			}
			output.PrintJSON(normalizeAgentValue(raw))
			return nil
		}

		var resp struct {
			Status int    `json:"status"`
			Msg    string `json:"msg"`
		}
		if err := json.Unmarshal(data, &resp); err != nil {
			return failArg("failed to parse response: " + err.Error())
		}
		if resp.Status != 0 && resp.Msg != "" {
			return failArg(resp.Msg)
		}
		output.Success(fmt.Sprintf("Archive task %d: %s", id, state))
		return nil
	},
}

// ─── archive once ──────────────────────────────────────────────────────────

var archiveOnceCmd = &cobra.Command{
	Use:   "once <ARCHIVE_ID>",
	Short: "Trigger an immediate one-time archive run",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := parseArchiveID(args[0])
		if err != nil {
			return err
		}

		form := url.Values{
			"archive_id": {strconv.Itoa(id)},
		}

		detail := map[string]any{"archiveId": strconv.Itoa(id)}
		if markDryRunOrConfirm("run archive once", detail) {
			return nil
		}

		client, _, _, err := newClient()
		if err != nil {
			return err
		}

		data, err := client.InternalGet(apiCtx(), "/archive/once/?"+form.Encode())
		if err != nil {
			return handleAPIError(err)
		}

		if jsonMode {
			fields := getFieldsFlag(cmd)
			var raw any
			if err := json.Unmarshal(data, &raw); err != nil {
				return failWithCode("parsing response: "+err.Error(), ExitNetwork, output.E_SERVER)
			}
			if m, ok := raw.(map[string]any); ok && len(fields) > 0 {
				output.PrintJSON(output.FilterMap(normalizeAgentMap(m), fields))
			} else {
				output.PrintJSON(normalizeAgentValue(raw))
			}
			return nil
		}

		var resp struct {
			Status int    `json:"status"`
			Msg    string `json:"msg"`
		}
		if err := json.Unmarshal(data, &resp); err != nil {
			return failArg("failed to parse response: " + err.Error())
		}
		if resp.Status != 0 && resp.Msg != "" {
			return failArg(resp.Msg)
		}
		output.Success(fmt.Sprintf("Archive task %d: one-time run triggered", id))
		return nil
	},
}

// ─── archive log ───────────────────────────────────────────────────────────

var archiveLogCmd = &cobra.Command{
	Use:   "log <ARCHIVE_ID>",
	Short: "View archive execution log",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := parseArchiveID(args[0])
		if err != nil {
			return err
		}

		client, _, _, err := newClient()
		if err != nil {
			return err
		}

		limit, _ := cmd.Flags().GetInt("limit")
		offset, _ := cmd.Flags().GetInt("offset")

		if limit < 1 || limit > 500 {
			return failArg("--limit must be between 1 and 500")
		}
		if offset < 0 {
			return failArg("--offset must be >= 0")
		}

		params := url.Values{
			"archive_id": {strconv.Itoa(id)},
			"limit":      {strconv.Itoa(limit)},
			"offset":     {strconv.Itoa(offset)},
		}

		data, err := client.InternalGet(apiCtx(), "/archive/log/?"+params.Encode())
		if err != nil {
			return handleAPIError(err)
		}

		if jsonMode {
			fields := getFieldsFlag(cmd)
			var raw any
			if err := json.Unmarshal(data, &raw); err != nil {
				return failWithCode("parsing response: "+err.Error(), ExitNetwork, output.E_SERVER)
			}
			items := extractArchiveLogItems(raw)
			total := extractArchiveTotal(raw)
			if len(fields) > 0 {
				filtered := make([]map[string]any, len(items))
				for i, m := range items {
					filtered[i] = output.FilterMap(m, fields)
				}
				items = filtered
			}
			output.PrintJSON(offsetListEnvelope(items, offset, len(items), total))
			return nil
		}

		// Text mode
		var resp struct {
			Status int    `json:"status"`
			Msg    string `json:"msg"`
			Data   struct {
				Total int              `json:"total"`
				Rows  []map[string]any `json:"rows"`
			} `json:"data"`
		}
		if err := json.Unmarshal(data, &resp); err != nil {
			return failArg("failed to parse response: " + err.Error())
		}
		if resp.Status != 0 && resp.Msg != "" {
			return failArg(resp.Msg)
		}
		if len(resp.Data.Rows) == 0 {
			output.Info("No archive log entries found.")
			return nil
		}

		headers := []string{"ID", "STATE", "START", "END", "ROWS"}
		rows := make([][]string, len(resp.Data.Rows))
		for i, r := range resp.Data.Rows {
			rows[i] = []string{
				fmt.Sprintf("%v", r["id"]),
				fmt.Sprintf("%v", r["state"]),
				fmt.Sprintf("%v", r["start_time"]),
				fmt.Sprintf("%v", r["end_time"]),
				fmt.Sprintf("%v", r["success_rows"]),
			}
		}
		output.Table(headers, rows)
		return nil
	},
}

// ─── helpers ───────────────────────────────────────────────────────────────

func parseArchiveID(s string) (int, error) {
	id, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || id <= 0 {
		return 0, failArg("ARCHIVE_ID must be a positive integer")
	}
	return id, nil
}

// extractArchiveItems extracts items from internal API response formats.
func extractArchiveItems(raw any) []map[string]any {
	if raw == nil {
		return nil
	}
	if m, ok := raw.(map[string]any); ok {
		// Try internal paginated: {data: {rows: [...]}}
		if data, ok := m["data"]; ok {
			if dm, ok := data.(map[string]any); ok {
				if rows, ok := dm["rows"]; ok {
					return extractRows(rows)
				}
			}
		}
		// Try flat: {rows: [...]}
		if rows, ok := m["rows"]; ok {
			return extractRows(rows)
		}
	}
	return extractRows(raw)
}

// extractArchiveLogItems extracts log items from internal API response formats.
func extractArchiveLogItems(raw any) []map[string]any {
	return extractArchiveItems(raw)
}

// extractArchiveTotal extracts the total count from an internal API paginated response.
func extractArchiveTotal(raw any) int {
	if m, ok := raw.(map[string]any); ok {
		if data, ok := m["data"]; ok {
			if dm, ok := data.(map[string]any); ok {
				if t, ok := dm["total"]; ok {
					if n, ok := t.(float64); ok {
						return int(n)
					}
				}
			}
		}
	}
	return 0
}

// extractRows converts a raw value into a slice of maps.
func extractRows(raw any) []map[string]any {
	if raw == nil {
		return nil
	}
	if arr, ok := raw.([]any); ok {
		items := make([]map[string]any, 0, len(arr))
		for _, v := range arr {
			if m, ok := v.(map[string]any); ok {
				items = append(items, normalizeAgentMap(m))
			}
		}
		return items
	}
	return nil
}
