package cmd

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"

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
	userListCmd.Flags().String("search", "", "Search by username")
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
}

// ─── user list ─────────────────────────────────────────────────────────────

// userResult mirrors the Archery REST API user JSON shape.
type userResult struct {
	ID       int    `json:"id"`
	Username string `json:"username"`
	Display  string `json:"display"`
	Email    string `json:"email"`
	IsStaff  bool   `json:"is_staff"`
	IsActive bool   `json:"is_active"`
}

var userListCmd = &cobra.Command{
	Use:   "list",
	Short: "List users",
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, _, err := newClient()
		if err != nil {
			return err
		}

		limit, _ := cmd.Flags().GetInt("limit")
		offset, _ := cmd.Flags().GetInt("offset")
		search, _ := cmd.Flags().GetString("search")

		if limit < 1 || limit > 500 {
			return failArg("--limit must be between 1 and 500")
		}
		if offset < 0 {
			return failArg("--offset must be >= 0")
		}

		params := url.Values{}
		params.Set("limit", strconv.Itoa(limit))
		params.Set("offset", strconv.Itoa(offset))
		if search != "" {
			params.Set("search", search)
		}

		path := client.APIPath("/v1/user/")
		if encoded := params.Encode(); encoded != "" {
			path += "?" + encoded
		}

		data, err := client.Get(apiCtx(), path)
		if err != nil {
			return handleAPIError(err)
		}

		var page struct {
			Count    int          `json:"count"`
			Next     any          `json:"next"`
			Previous any          `json:"previous"`
			Results  []userResult `json:"results"`
		}
		if err := json.Unmarshal(data, &page); err != nil {
			return failWithCode("parsing response: "+err.Error(), ExitNetwork, output.E_SERVER)
		}

		if jsonMode {
			fields := getFieldsFlag(cmd)
			items := make([]map[string]any, len(page.Results))
			for i, u := range page.Results {
				items[i] = userResultToMap(u)
			}
			if len(fields) > 0 {
				filtered := make([]map[string]any, len(items))
				for i, m := range items {
					filtered[i] = output.FilterMap(m, fields)
				}
				items = filtered
			}
			output.PrintJSON(output.NewListEnvelope(items, output.ListMeta{
				Count:   len(page.Results),
				Limit:   limit,
				Total:   page.Count,
				HasMore: page.Next != nil,
			}))
			return nil
		}

		if len(page.Results) == 0 {
			output.Info("No users found.")
			return nil
		}
		headers := []string{"ID", "USERNAME", "DISPLAY", "EMAIL", "ACTIVE"}
		rows := make([][]string, len(page.Results))
		for i, u := range page.Results {
			rows[i] = []string{
				strconv.Itoa(u.ID),
				u.Username,
				u.Display,
				u.Email,
				fmt.Sprintf("%t", u.IsActive),
			}
		}
		output.Table(headers, rows)
		return nil
	},
}

// ─── user groups ───────────────────────────────────────────────────────────

// groupResult mirrors the Archery REST API group JSON shape.
type groupResult struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

var userGroupsCmd = &cobra.Command{
	Use:   "groups",
	Short: "List user groups",
	RunE: func(cmd *cobra.Command, args []string) error {
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

		params := url.Values{}
		params.Set("limit", strconv.Itoa(limit))
		params.Set("offset", strconv.Itoa(offset))

		path := client.APIPath("/v1/user/group/")
		if encoded := params.Encode(); encoded != "" {
			path += "?" + encoded
		}

		data, err := client.Get(apiCtx(), path)
		if err != nil {
			return handleAPIError(err)
		}

		var page struct {
			Count    int           `json:"count"`
			Next     any           `json:"next"`
			Previous any           `json:"previous"`
			Results  []groupResult `json:"results"`
		}
		if err := json.Unmarshal(data, &page); err != nil {
			return failWithCode("parsing response: "+err.Error(), ExitNetwork, output.E_SERVER)
		}

		if jsonMode {
			fields := getFieldsFlag(cmd)
			items := make([]map[string]any, len(page.Results))
			for i, g := range page.Results {
				items[i] = groupResultToMap(g)
			}
			if len(fields) > 0 {
				filtered := make([]map[string]any, len(items))
				for i, m := range items {
					filtered[i] = output.FilterMap(m, fields)
				}
				items = filtered
			}
			output.PrintJSON(output.NewListEnvelope(items, output.ListMeta{
				Count:   len(page.Results),
				Limit:   limit,
				Total:   page.Count,
				HasMore: page.Next != nil,
			}))
			return nil
		}

		if len(page.Results) == 0 {
			output.Info("No groups found.")
			return nil
		}
		headers := []string{"ID", "NAME"}
		rows := make([][]string, len(page.Results))
		for i, g := range page.Results {
			rows[i] = []string{
				strconv.Itoa(g.ID),
				g.Name,
			}
		}
		output.Table(headers, rows)
		return nil
	},
}

// ─── user resource-groups ──────────────────────────────────────────────────

// resourceGroupResult mirrors the Archery REST API resource group JSON shape.
type resourceGroupResult struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	ActiveUsers int    `json:"active_users"`
	BindUsers   int    `json:"bind_users"`
}

var userResourceGroupsCmd = &cobra.Command{
	Use:   "resource-groups",
	Short: "List resource groups",
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, _, err := newClient()
		if err != nil {
			return err
		}

		limit, _ := cmd.Flags().GetInt("limit")
		offset, _ := cmd.Flags().GetInt("offset")
		search, _ := cmd.Flags().GetString("search")

		if limit < 1 || limit > 500 {
			return failArg("--limit must be between 1 and 500")
		}
		if offset < 0 {
			return failArg("--offset must be >= 0")
		}

		params := url.Values{}
		params.Set("limit", strconv.Itoa(limit))
		params.Set("offset", strconv.Itoa(offset))
		if search != "" {
			params.Set("search", search)
		}

		path := client.APIPath("/v1/user/resourcegroup/")
		if encoded := params.Encode(); encoded != "" {
			path += "?" + encoded
		}

		data, err := client.Get(apiCtx(), path)
		if err != nil {
			return handleAPIError(err)
		}

		var page struct {
			Count    int                   `json:"count"`
			Next     any                   `json:"next"`
			Previous any                   `json:"previous"`
			Results  []resourceGroupResult `json:"results"`
		}
		if err := json.Unmarshal(data, &page); err != nil {
			return failWithCode("parsing response: "+err.Error(), ExitNetwork, output.E_SERVER)
		}

		if jsonMode {
			fields := getFieldsFlag(cmd)
			items := make([]map[string]any, len(page.Results))
			for i, rg := range page.Results {
				items[i] = resourceGroupResultToMap(rg)
			}
			if len(fields) > 0 {
				filtered := make([]map[string]any, len(items))
				for i, m := range items {
					filtered[i] = output.FilterMap(m, fields)
				}
				items = filtered
			}
			output.PrintJSON(output.NewListEnvelope(items, output.ListMeta{
				Count:   len(page.Results),
				Limit:   limit,
				Total:   page.Count,
				HasMore: page.Next != nil,
			}))
			return nil
		}

		if len(page.Results) == 0 {
			output.Info("No resource groups found.")
			return nil
		}
		headers := []string{"ID", "NAME", "ACTIVE_USERS", "BIND_USERS"}
		rows := make([][]string, len(page.Results))
		for i, rg := range page.Results {
			rows[i] = []string{
				strconv.Itoa(rg.ID),
				rg.Name,
				strconv.Itoa(rg.ActiveUsers),
				strconv.Itoa(rg.BindUsers),
			}
		}
		output.Table(headers, rows)
		return nil
	},
}

// ─── helpers ───────────────────────────────────────────────────────────────

func userResultToMap(u userResult) map[string]any {
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

func groupResultToMap(g groupResult) map[string]any {
	m := map[string]any{
		"id":   strconv.Itoa(g.ID),
		"name": g.Name,
	}
	tagCommonUntrusted(m)
	return m
}

func resourceGroupResultToMap(rg resourceGroupResult) map[string]any {
	m := map[string]any{
		"id":          strconv.Itoa(rg.ID),
		"name":        rg.Name,
		"activeUsers": rg.ActiveUsers,
		"bindUsers":   rg.BindUsers,
	}
	tagCommonUntrusted(m)
	return m
}
