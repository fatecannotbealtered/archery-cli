package cmd

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/fatecannotbealtered/archery-cli/internal/api"
	"github.com/fatecannotbealtered/archery-cli/internal/output"
	"github.com/spf13/cobra"
)

var userCmd = &cobra.Command{
	Use:   "user",
	Short: "Manage users, groups, and resource groups",
}

func init() {
	rootCmd.AddCommand(userCmd)

	// user list
	userListCmd.Flags().Int("limit", 20, "Max results (1-500)")
	userListCmd.Flags().Int("offset", 0, "Pagination offset")
	userListCmd.Flags().String("search", "", "Search by username/display/email")
	userListCmd.Flags().String("fields", "", "Comma-separated fields for JSON output")
	userCmd.AddCommand(userListCmd)

	// user groups
	userGroupsCmd.Flags().Int("limit", 20, "Max results (1-500)")
	userGroupsCmd.Flags().Int("offset", 0, "Pagination offset")
	userGroupsCmd.Flags().String("fields", "", "Comma-separated fields for JSON output")
	userCmd.AddCommand(userGroupsCmd)

	// user resource-groups
	userResourceGroupsCmd.Flags().Int("limit", 20, "Max results (1-500)")
	userResourceGroupsCmd.Flags().Int("offset", 0, "Pagination offset")
	userResourceGroupsCmd.Flags().String("search", "", "Search by group name")
	userResourceGroupsCmd.Flags().String("fields", "", "Comma-separated fields for JSON output")
	userCmd.AddCommand(userResourceGroupsCmd)

	// user resourcegroup-add
	userResourceGroupAddCmd.Flags().Int("group", 0, "Resource group id (required)")
	userResourceGroupAddCmd.Flags().String("type", "user", "Object type to associate: user or instance")
	userResourceGroupAddCmd.Flags().String("ids", "", "Comma-separated user/instance ids to add (required)")
	userCmd.AddCommand(userResourceGroupAddCmd)
	markWrite(userResourceGroupAddCmd)
	// Permission grant: reversible and non-destructive, so medium risk (no
	// --dangerous gate), but still dry-run/confirm gated as a write.
	markRiskLevel(userResourceGroupAddCmd, "medium")
}

// ─── user list ─────────────────────────────────────────────────────────────

var userListCmd = &cobra.Command{
	Use:   "list",
	Short: "List users",
	RunE: func(cmd *cobra.Command, args []string) error {
		limit, _ := cmd.Flags().GetInt("limit")
		offset, _ := cmd.Flags().GetInt("offset")
		search, _ := cmd.Flags().GetString("search")

		if limit < 1 || limit > 500 {
			return failArg("--limit must be between 1 and 500")
		}
		if offset < 0 {
			return failArg("--offset must be >= 0")
		}

		client, _, _, err := newClient()
		if err != nil {
			return err
		}

		rows, err := client.Users.ListUsers(apiCtx())
		if err != nil {
			return handleAPIError(err)
		}

		// The session endpoint returns the full list without server-side search or
		// pagination, so apply both client-side; harmless when REST already paged.
		rows = filterUsers(rows, search)
		total := len(rows)
		rows = pageUsers(rows, offset, limit)

		if jsonMode {
			fields := getFieldsFlag(cmd)
			items := make([]map[string]any, len(rows))
			for i, u := range rows {
				items[i] = userRowToMap(u)
			}
			if len(fields) > 0 {
				for i, m := range items {
					items[i] = output.FilterMap(m, fields)
				}
			}
			output.PrintJSON(output.NewListEnvelope(items, output.ListMeta{
				Count:   len(rows),
				Limit:   limit,
				Total:   total,
				HasMore: offset+len(rows) < total,
			}))
			return nil
		}

		if len(rows) == 0 {
			output.Info("No users found.")
			return nil
		}
		headers := []string{"ID", "USERNAME", "DISPLAY", "EMAIL", "ACTIVE"}
		out := make([][]string, len(rows))
		for i, u := range rows {
			out[i] = []string{
				strconv.Itoa(u.ID),
				u.Username,
				u.Display,
				u.Email,
				fmt.Sprintf("%t", u.IsActive),
			}
		}
		output.Table(headers, out)
		return nil
	},
}

// ─── user groups ───────────────────────────────────────────────────────────

var userGroupsCmd = &cobra.Command{
	Use:   "groups",
	Short: "List permission groups (requires --mode jwt)",
	RunE: func(cmd *cobra.Command, args []string) error {
		limit, _ := cmd.Flags().GetInt("limit")
		offset, _ := cmd.Flags().GetInt("offset")

		if limit < 1 || limit > 500 {
			return failArg("--limit must be between 1 and 500")
		}
		if offset < 0 {
			return failArg("--offset must be >= 0")
		}

		client, _, _, err := newClient()
		if err != nil {
			return err
		}

		// v1.8.5 exposes no session JSON route for Django auth groups (the web
		// /group/ page renders HTML). Gate session mode rather than issue a request
		// that would return markup; JWT mode hits the REST list endpoint.
		if client.Mode() != api.ModeJWT {
			return failWithCode(
				"permission group listing has no session endpoint in Archery v1.8.5; rerun with --mode jwt",
				output.E_NOT_FOUND)
		}

		rows, total, err := client.Users.ListGroups(apiCtx(), limit, offset)
		if err != nil {
			return handleAPIError(err)
		}

		if jsonMode {
			fields := getFieldsFlag(cmd)
			items := make([]map[string]any, len(rows))
			for i, g := range rows {
				items[i] = groupRowToMap(g)
			}
			if len(fields) > 0 {
				for i, m := range items {
					items[i] = output.FilterMap(m, fields)
				}
			}
			output.PrintJSON(output.NewListEnvelope(items, output.ListMeta{
				Count:   len(rows),
				Limit:   limit,
				Total:   total,
				HasMore: offset+len(rows) < total,
			}))
			return nil
		}

		if len(rows) == 0 {
			output.Info("No groups found.")
			return nil
		}
		headers := []string{"ID", "NAME"}
		out := make([][]string, len(rows))
		for i, g := range rows {
			out[i] = []string{strconv.Itoa(g.ID), g.Name}
		}
		output.Table(headers, out)
		return nil
	},
}

// ─── user resource-groups ──────────────────────────────────────────────────

var userResourceGroupsCmd = &cobra.Command{
	Use:   "resource-groups",
	Short: "List resource groups",
	RunE: func(cmd *cobra.Command, args []string) error {
		limit, _ := cmd.Flags().GetInt("limit")
		offset, _ := cmd.Flags().GetInt("offset")
		search, _ := cmd.Flags().GetString("search")

		if limit < 1 || limit > 500 {
			return failArg("--limit must be between 1 and 500")
		}
		if offset < 0 {
			return failArg("--offset must be >= 0")
		}

		client, _, _, err := newClient()
		if err != nil {
			return err
		}

		rows, total, err := client.Users.ListResourceGroups(apiCtx(), limit, offset, search)
		if err != nil {
			return handleAPIError(err)
		}

		if jsonMode {
			fields := getFieldsFlag(cmd)
			items := make([]map[string]any, len(rows))
			for i, rg := range rows {
				items[i] = resourceGroupRowToMap(rg)
			}
			if len(fields) > 0 {
				for i, m := range items {
					items[i] = output.FilterMap(m, fields)
				}
			}
			output.PrintJSON(output.NewListEnvelope(items, output.ListMeta{
				Count:   len(rows),
				Limit:   limit,
				Total:   total,
				HasMore: offset+len(rows) < total,
			}))
			return nil
		}

		if len(rows) == 0 {
			output.Info("No resource groups found.")
			return nil
		}
		headers := []string{"ID", "NAME", "ACTIVE_USERS", "BIND_USERS"}
		out := make([][]string, len(rows))
		for i, rg := range rows {
			out[i] = []string{
				strconv.Itoa(rg.ID),
				rg.Name,
				strconv.Itoa(rg.ActiveUsers),
				strconv.Itoa(rg.BindUsers),
			}
		}
		output.Table(headers, out)
		return nil
	},
}

// ─── user resourcegroup-add ────────────────────────────────────────────────

var userResourceGroupAddCmd = &cobra.Command{
	Use:   "resourcegroup-add",
	Short: "Associate users or instances with a resource group",
	Long: "Grants a resource group access to one or more users (--type user) or " +
		"database instances (--type instance). Session-only; requires admin/DBA " +
		"privileges. Idempotent: re-adding an existing member is a no-op upstream.",
	RunE: func(cmd *cobra.Command, args []string) error {
		groupID, _ := cmd.Flags().GetInt("group")
		if groupID <= 0 {
			return failArg("--group must be a positive resource group id")
		}
		typeFlag, _ := cmd.Flags().GetString("type")
		objectType, label, err := parseResourceGroupObjectType(typeFlag)
		if err != nil {
			return err
		}
		idsFlag, _ := cmd.Flags().GetString("ids")
		if strings.TrimSpace(idsFlag) == "" {
			return failArg("--ids is required")
		}
		ids, err := parseIntIDs(idsFlag)
		if err != nil {
			return err
		}

		detail := map[string]any{
			"groupId":    groupID,
			"objectType": label,
			"ids":        ids,
		}
		if markDryRunOrConfirm(fmt.Sprintf("add %d %s(s) to resource group %d", len(ids), label, groupID), detail) {
			return nil
		}

		client, _, _, err := newClient()
		if err != nil {
			return err
		}

		if err := client.Users.AddResourceGroupRelation(apiCtx(), groupID, objectType, ids); err != nil {
			return handleAPIError(err)
		}

		if jsonMode {
			output.PrintJSON(map[string]any{
				"groupId":    groupID,
				"objectType": label,
				"added":      ids,
				"count":      len(ids),
			})
			return nil
		}
		output.Success(fmt.Sprintf("Added %d %s(s) to resource group %d", len(ids), label, groupID))
		return nil
	},
}

// ─── helpers ───────────────────────────────────────────────────────────────

// parseResourceGroupObjectType maps the --type flag to Archery's object_type
// code and a human-readable label.
func parseResourceGroupObjectType(s string) (api.ResourceGroupObjectType, string, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "user", "users", "0":
		return api.ResourceGroupObjectUser, "user", nil
	case "instance", "instances", "1":
		return api.ResourceGroupObjectInstance, "instance", nil
	default:
		return "", "", failArg("--type must be 'user' or 'instance'")
	}
}

// parseIntIDs splits a comma-separated id list into positive integers.
func parseIntIDs(s string) ([]int, error) {
	parts := splitCSV(s)
	ids := make([]int, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil || n <= 0 {
			return nil, failArg(fmt.Sprintf("invalid id %q: must be a positive integer", p))
		}
		ids = append(ids, n)
	}
	if len(ids) == 0 {
		return nil, failArg("--ids must contain at least one id")
	}
	return ids, nil
}

// filterUsers keeps users whose username/display/email contains search
// (case-insensitive). An empty search returns the input unchanged.
func filterUsers(rows []api.UserRow, search string) []api.UserRow {
	if search == "" {
		return rows
	}
	needle := strings.ToLower(search)
	out := make([]api.UserRow, 0, len(rows))
	for _, u := range rows {
		if strings.Contains(strings.ToLower(u.Username), needle) ||
			strings.Contains(strings.ToLower(u.Display), needle) ||
			strings.Contains(strings.ToLower(u.Email), needle) {
			out = append(out, u)
		}
	}
	return out
}

// pageUsers applies a client-side offset/limit window.
func pageUsers(rows []api.UserRow, offset, limit int) []api.UserRow {
	if offset >= len(rows) {
		return nil
	}
	end := offset + limit
	if end > len(rows) {
		end = len(rows)
	}
	return rows[offset:end]
}

func userRowToMap(u api.UserRow) map[string]any {
	m := map[string]any{
		"id":       strconv.Itoa(u.ID),
		"username": u.Username,
		"isActive": u.IsActive,
	}
	if u.Display != "" {
		m["display"] = u.Display
	}
	if u.Email != "" {
		m["email"] = u.Email
	}
	m["isStaff"] = u.IsStaff
	tagCommonUntrusted(m)
	return m
}

func groupRowToMap(g api.GroupRow) map[string]any {
	m := map[string]any{
		"id":   strconv.Itoa(g.ID),
		"name": g.Name,
	}
	tagCommonUntrusted(m)
	return m
}

func resourceGroupRowToMap(rg api.ResourceGroupRow) map[string]any {
	m := map[string]any{
		"id":          strconv.Itoa(rg.ID),
		"name":        rg.Name,
		"activeUsers": rg.ActiveUsers,
		"bindUsers":   rg.BindUsers,
	}
	tagCommonUntrusted(m)
	return m
}
