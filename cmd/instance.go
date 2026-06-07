package cmd

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/fatecannotbealtered/archery-cli/internal/api"
	"github.com/fatecannotbealtered/archery-cli/internal/output"
	"github.com/spf13/cobra"
)

var instanceCmd = &cobra.Command{
	Use:   "instance",
	Short: "Manage database instances",
}

func init() {
	rootCmd.AddCommand(instanceCmd)

	// instance list
	instanceListCmd.Flags().String("type", "", "Filter by instance type: master|slave")
	instanceListCmd.Flags().String("db-type", "", "Filter by database type: mysql|pgsql|mssql|redis|...")
	instanceListCmd.Flags().String("search", "", "Search by instance name")
	instanceListCmd.Flags().Int("limit", 20, "Max results per page (1-500)")
	instanceListCmd.Flags().Int("offset", 0, "Pagination offset")
	instanceListCmd.Flags().String("fields", "", "Comma-separated fields for JSON output")
	instanceCmd.AddCommand(instanceListCmd)

	// instance detail
	instanceDetailCmd.Flags().String("fields", "", "Comma-separated fields for JSON output")
	instanceCmd.AddCommand(instanceDetailCmd)

	// instance resource
	instanceResourceCmd.Flags().Int("instance", 0, "Instance ID (required)")
	instanceResourceCmd.Flags().String("type", "", "Resource type: database|schema|table|column (required)")
	instanceResourceCmd.Flags().String("db", "", "Database name")
	instanceResourceCmd.Flags().String("schema", "", "Schema name")
	instanceResourceCmd.Flags().String("table", "", "Table name")
	instanceResourceCmd.Flags().String("fields", "", "Comma-separated fields for JSON output")
	instanceCmd.AddCommand(instanceResourceCmd)

	// instance describe
	instanceDescribeCmd.Flags().String("instance", "", "Instance name (required)")
	instanceDescribeCmd.Flags().String("db", "", "Database name (required)")
	instanceDescribeCmd.Flags().String("table", "", "Table name (required)")
	instanceDescribeCmd.Flags().String("schema", "", "Schema name")
	instanceDescribeCmd.Flags().String("fields", "", "Comma-separated fields for JSON output")
	instanceCmd.AddCommand(instanceDescribeCmd)

	// instance create
	instanceCreateCmd.Flags().String("name", "", "Instance name (required)")
	instanceCreateCmd.Flags().String("type", "", "Instance type: master|slave (required)")
	instanceCreateCmd.Flags().String("db-type", "", "Database type: mysql|pgsql|mssql|redis|... (required)")
	instanceCreateCmd.Flags().String("host", "", "Host address (required)")
	instanceCreateCmd.Flags().Int("port", 0, "Port number (required)")
	instanceCreateCmd.Flags().String("user", "", "Database user (required)")
	instanceCreateCmd.Flags().String("password", "", "Database password")
	instanceCreateCmd.Flags().String("mode", "", "Mode: standalone|cluster")
	instanceCreateCmd.Flags().String("db", "", "Default database name")
	instanceCreateCmd.Flags().String("charset", "", "Character set")
	instanceCmd.AddCommand(instanceCreateCmd)
	markWrite(instanceCreateCmd)
	markConfirm(instanceCreateCmd)
	markRiskLevel(instanceCreateCmd, "high")

	// instance update
	instanceUpdateCmd.Flags().String("name", "", "New instance name")
	instanceUpdateCmd.Flags().String("host", "", "New host address")
	instanceUpdateCmd.Flags().Int("port", 0, "New port number")
	instanceUpdateCmd.Flags().String("user", "", "New database user")
	instanceUpdateCmd.Flags().String("password", "", "New database password")
	instanceCmd.AddCommand(instanceUpdateCmd)
	markWrite(instanceUpdateCmd)
	markConfirm(instanceUpdateCmd)

	// instance delete
	instanceCmd.AddCommand(instanceDeleteCmd)
	markWrite(instanceDeleteCmd)
	markConfirm(instanceDeleteCmd)
	markRiskLevel(instanceDeleteCmd, "high")

	// instance table-instances
	instanceTableInstancesCmd.Flags().String("table", "", "Table name to search for (required)")
	instanceCmd.AddCommand(instanceTableInstancesCmd)

	// instance users
	instanceUsersCmd.Flags().Int("instance", 0, "Instance ID (required)")
	instanceUsersCmd.Flags().Bool("saved", false, "Filter by saved users only")
	instanceCmd.AddCommand(instanceUsersCmd)
}

// ─── instance list ──────────────────────────────────────────────────────────

var instanceListCmd = &cobra.Command{
	Use:   "list",
	Short: "List database instances",
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, _, err := newClient()
		if err != nil {
			return err
		}

		instanceType, _ := cmd.Flags().GetString("type")
		dbType, _ := cmd.Flags().GetString("db-type")
		search, _ := cmd.Flags().GetString("search")
		limit, _ := cmd.Flags().GetInt("limit")
		offset, _ := cmd.Flags().GetInt("offset")

		if limit < 1 || limit > 500 {
			return failArg("--limit must be between 1 and 500")
		}
		if offset < 0 {
			return failArg("--offset must be >= 0")
		}

		params := url.Values{}
		if instanceType != "" {
			params.Set("instance_type", instanceType)
		}
		if dbType != "" {
			params.Set("db_type", dbType)
		}
		if search != "" {
			params.Set("search", search)
		}
		params.Set("limit", strconv.Itoa(limit))
		params.Set("offset", strconv.Itoa(offset))

		path := client.APIPath("/v1/instance/")
		if encoded := params.Encode(); encoded != "" {
			path += "?" + encoded
		}

		data, err := client.Get(apiCtx(), path)
		if err != nil {
			return handleAPIError(err)
		}

		var page struct {
			Count    int              `json:"count"`
			Next     any              `json:"next"`
			Previous any              `json:"previous"`
			Results  []instanceResult `json:"results"`
		}
		if err := json.Unmarshal(data, &page); err != nil {
			return failWithCode("parsing response: "+err.Error(), ExitError, output.E_SERVER)
		}

		if jsonMode {
			fields := getFieldsFlag(cmd)
			items := make([]map[string]any, len(page.Results))
			for i, inst := range page.Results {
				items[i] = instanceResultToMap(inst)
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
			output.Info("No instances found.")
			return nil
		}
		headers := []string{"ID", "NAME", "DB TYPE", "HOST", "PORT", "USER"}
		rows := make([][]string, len(page.Results))
		for i, inst := range page.Results {
			rows[i] = []string{
				inst.ID,
				inst.InstanceName,
				inst.DbType,
				inst.Host,
				strconv.Itoa(inst.Port),
				inst.User,
			}
		}
		output.Table(headers, rows)
		return nil
	},
}

// ─── instance detail ────────────────────────────────────────────────────────

var instanceDetailCmd = &cobra.Command{
	Use:   "detail <INSTANCE_ID>",
	Short: "Show instance details",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := parseInstanceID(args[0])
		if err != nil {
			return err
		}

		client, _, _, err := newClient()
		if err != nil {
			return err
		}

		path := client.APIPath(fmt.Sprintf("/v1/instance/%s/", id))
		data, err := client.Get(apiCtx(), path)
		if err != nil {
			return handleAPIError(err)
		}

		var inst instanceResult
		if err := json.Unmarshal(data, &inst); err != nil {
			return failWithCode("parsing response: "+err.Error(), ExitError, output.E_SERVER)
		}

		if jsonMode {
			fields := getFieldsFlag(cmd)
			m := instanceResultToMap(inst)
			if len(fields) > 0 {
				m = output.FilterMap(m, fields)
			}
			output.PrintJSON(m)
			return nil
		}

		fmt.Println()
		output.Bold(fmt.Sprintf("  %s (#%s)", inst.InstanceName, inst.ID))
		output.Gray("  ────────────────────────────────────────")
		printInstanceField("DB Type", inst.DbType)
		printInstanceField("Host", fmt.Sprintf("%s:%d", inst.Host, inst.Port))
		printInstanceField("User", inst.User)
		printInstanceField("DB Name", inst.DBName)
		printInstanceField("Charset", inst.Charset)
		printInstanceField("Environment", inst.Environment)
		printInstanceField("Active", fmt.Sprintf("%t", inst.IsActive))
		fmt.Println()
		return nil
	},
}

// ─── instance resource ──────────────────────────────────────────────────────

var instanceResourceCmd = &cobra.Command{
	Use:   "resource",
	Short: "List databases, schemas, tables, or columns on an instance",
	RunE: func(cmd *cobra.Command, args []string) error {
		instanceID, _ := cmd.Flags().GetInt("instance")
		if instanceID == 0 {
			return failArg("--instance is required")
		}
		resourceType, _ := cmd.Flags().GetString("type")
		if resourceType == "" {
			return failArg("--type is required")
		}
		resourceType = strings.ToLower(resourceType)
		switch resourceType {
		case "database", "schema", "table", "column":
		default:
			return failArg("--type must be one of: database, schema, table, column")
		}

		dbName, _ := cmd.Flags().GetString("db")
		schemaName, _ := cmd.Flags().GetString("schema")
		tableName, _ := cmd.Flags().GetString("table")

		client, _, _, err := newClient()
		if err != nil {
			return err
		}

		payload := map[string]any{
			"instance_id":   instanceID,
			"resource_type": resourceType,
		}
		if dbName != "" {
			payload["db_name"] = dbName
		}
		if schemaName != "" {
			payload["schema_name"] = schemaName
		}
		if tableName != "" {
			payload["table_name"] = tableName
		}

		data, err := client.Post(apiCtx(), client.APIPath("/v1/instance/resource/"), payload)
		if err != nil {
			return handleAPIError(err)
		}

		if jsonMode {
			fields := getFieldsFlag(cmd)
			var raw any
			if err := json.Unmarshal(data, &raw); err != nil {
				return failWithCode("parsing response: "+err.Error(), ExitError, output.E_SERVER)
			}
			// Try to extract the list from the response
			items := extractResourceItems(raw)
			if len(fields) > 0 && len(items) > 0 {
				filtered := make([]map[string]any, len(items))
				for i, m := range items {
					filtered[i] = output.FilterMap(m, fields)
				}
				items = filtered
			}
			output.PrintJSON(items)
			return nil
		}

		// Text mode: print as table
		items := extractResourceItems(nil)
		_ = json.Unmarshal(data, &items)
		if len(items) == 0 {
			output.Info("No resources found.")
			return nil
		}
		printResourceTable(items)
		return nil
	},
}

// ─── instance describe ──────────────────────────────────────────────────────

var instanceDescribeCmd = &cobra.Command{
	Use:   "describe",
	Short: "Describe table structure (DESCRIBE)",
	RunE: func(cmd *cobra.Command, args []string) error {
		instanceName, _ := cmd.Flags().GetString("instance")
		if instanceName == "" {
			return failArg("--instance is required")
		}
		dbName, _ := cmd.Flags().GetString("db")
		if dbName == "" {
			return failArg("--db is required")
		}
		tableName, _ := cmd.Flags().GetString("table")
		if tableName == "" {
			return failArg("--table is required")
		}
		schemaName, _ := cmd.Flags().GetString("schema")

		client, _, _, err := newClient()
		if err != nil {
			return err
		}

		form := url.Values{
			"instance_name": {instanceName},
			"db_name":       {dbName},
			"table_name":    {tableName},
		}
		if schemaName != "" {
			form.Set("schema_name", schemaName)
		}

		data, err := client.InternalPost(apiCtx(), "/instance/describetable/", form)
		if err != nil {
			return handleAPIError(err)
		}

		if jsonMode {
			fields := getFieldsFlag(cmd)
			var raw any
			if err := json.Unmarshal(data, &raw); err != nil {
				return failWithCode("parsing response: "+err.Error(), ExitError, output.E_SERVER)
			}
			if cols, ok := raw.([]any); ok {
				for _, col := range cols {
					if cm, ok := col.(map[string]any); ok {
						api.TagUntrusted(cm, "comment", "Comment")
					}
				}
			}
			m := map[string]any{
				"instance": instanceName,
				"db":       dbName,
				"table":    tableName,
				"columns":  raw,
			}
			if schemaName != "" {
				m["schema"] = schemaName
			}
			if len(fields) > 0 {
				m = output.FilterMap(m, fields)
			}
			output.PrintJSON(m)
			return nil
		}

		// Text mode
		var rows []map[string]any
		if err := json.Unmarshal(data, &rows); err != nil {
			// Try internal response format
			var resp struct {
				Status int    `json:"status"`
				Msg    string `json:"msg"`
				Data   json.RawMessage `json:"data"`
			}
			if err2 := json.Unmarshal(data, &resp); err2 == nil && resp.Data != nil {
				_ = json.Unmarshal(resp.Data, &rows)
			}
		}
		if len(rows) == 0 {
			output.Info("No column information found.")
			return nil
		}
		printDescribeTable(rows)
		return nil
	},
}

// ─── instance create ────────────────────────────────────────────────────────

var instanceCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new database instance",
	RunE: func(cmd *cobra.Command, args []string) error {
		name, err := requireFlagString(cmd, "name", "--name")
		if err != nil {
			return err
		}
		instanceType, err := requireFlagString(cmd, "type", "--type")
		if err != nil {
			return err
		}
		instanceType = strings.ToLower(instanceType)
		if instanceType != "master" && instanceType != "slave" {
			return failArg("--type must be 'master' or 'slave'")
		}
		dbType, err := requireFlagString(cmd, "db-type", "--db-type")
		if err != nil {
			return err
		}
		host, err := requireFlagString(cmd, "host", "--host")
		if err != nil {
			return err
		}
		port, _ := cmd.Flags().GetInt("port")
		if port <= 0 || port > 65535 {
			return failArg("--port must be between 1 and 65535")
		}
		user, err := requireFlagString(cmd, "user", "--user")
		if err != nil {
			return err
		}
		password, _ := cmd.Flags().GetString("password")
		mode, _ := cmd.Flags().GetString("mode")
		dbName, _ := cmd.Flags().GetString("db")
		charset, _ := cmd.Flags().GetString("charset")

		client, _, _, err := newClient()
		if err != nil {
			return err
		}

		payload := map[string]any{
			"instance_name": name,
			"type":          0, // 0=master in Archery
			"db_type":       dbType,
			"host":          host,
			"port":          port,
			"user":          user,
		}
		if instanceType == "slave" {
			payload["type"] = 1
		}
		if password != "" {
			payload["password"] = password
		}
		if mode != "" {
			payload["mode"] = mode
		}
		if dbName != "" {
			payload["db_name"] = dbName
		}
		if charset != "" {
			payload["charset"] = charset
		}

		detail := map[string]any{
			"name":    name,
			"type":    instanceType,
			"dbType":  dbType,
			"host":    host,
			"port":    port,
			"user":    user,
		}
		if markDryRunOrConfirm("create instance", detail) {
			return nil
		}

		path := client.APIPath("/v1/instance/")
		data, err := client.Post(apiCtx(), path, payload)
		if err != nil {
			return handleAPIError(err)
		}

		var inst instanceResult
		if err := json.Unmarshal(data, &inst); err != nil {
			return failWithCode("parsing response: "+err.Error(), ExitError, output.E_SERVER)
		}

		if jsonMode {
			output.PrintJSON(instanceResultToMap(inst))
			return nil
		}
		output.Success(fmt.Sprintf("Instance %q created (ID: %s)", inst.InstanceName, inst.ID))
		return nil
	},
}

// ─── instance update ────────────────────────────────────────────────────────

var instanceUpdateCmd = &cobra.Command{
	Use:   "update <INSTANCE_ID>",
	Short: "Update an existing instance",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := parseInstanceID(args[0])
		if err != nil {
			return err
		}

		client, _, _, err := newClient()
		if err != nil {
			return err
		}

		payload := map[string]any{}
		if name, _ := cmd.Flags().GetString("name"); name != "" {
			payload["instance_name"] = name
		}
		if host, _ := cmd.Flags().GetString("host"); host != "" {
			payload["host"] = host
		}
		if port, _ := cmd.Flags().GetInt("port"); port > 0 {
			payload["port"] = port
		}
		if user, _ := cmd.Flags().GetString("user"); user != "" {
			payload["user"] = user
		}
		if password, _ := cmd.Flags().GetString("password"); password != "" {
			payload["password"] = password
		}

		if len(payload) == 0 {
			return failArg("at least one field to update is required")
		}

		detail := map[string]any{"instanceId": id}
		for k, v := range payload {
			if k != "password" {
				detail[k] = v
			} else {
				detail[k] = "***"
			}
		}
		if markDryRunOrConfirm("update instance", detail) {
			return nil
		}

		path := client.APIPath(fmt.Sprintf("/v1/instance/%s/", id))
		data, err := client.Put(apiCtx(), path, payload)
		if err != nil {
			return handleAPIError(err)
		}

		var inst instanceResult
		if err := json.Unmarshal(data, &inst); err != nil {
			return failWithCode("parsing response: "+err.Error(), ExitError, output.E_SERVER)
		}

		if jsonMode {
			output.PrintJSON(instanceResultToMap(inst))
			return nil
		}
		output.Success(fmt.Sprintf("Instance %q updated (ID: %s)", inst.InstanceName, inst.ID))
		return nil
	},
}

// ─── instance delete ────────────────────────────────────────────────────────

var instanceDeleteCmd = &cobra.Command{
	Use:   "delete <INSTANCE_ID>",
	Short: "Delete a database instance",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := parseInstanceID(args[0])
		if err != nil {
			return err
		}

		client, _, _, err := newClient()
		if err != nil {
			return err
		}

		if markDryRunOrConfirm("delete instance", map[string]any{"instanceId": id}) {
			return nil
		}

		path := client.APIPath(fmt.Sprintf("/v1/instance/%s/", id))
		if err := client.Delete(apiCtx(), path); err != nil {
			return handleAPIError(err)
		}

		if jsonMode {
			output.PrintJSON(map[string]any{
				"deleted":    true,
				"instanceId": id,
			})
			return nil
		}
		output.Success(fmt.Sprintf("Instance %s deleted", id))
		return nil
	},
}

// ─── instance table-instances ───────────────────────────────────────────────

var instanceTableInstancesCmd = &cobra.Command{
	Use:   "table-instances",
	Short: "Find which instances contain a given table",
	RunE: func(cmd *cobra.Command, args []string) error {
		tableName, _ := cmd.Flags().GetString("table")
		if tableName == "" {
			return failArg("--table is required")
		}

		client, _, _, err := newClient()
		if err != nil {
			return err
		}

		payload := map[string]any{
			"table_name": tableName,
		}

		data, err := client.Post(apiCtx(), client.APIPath("/v1/instance/table-instances/"), payload)
		if err != nil {
			return handleAPIError(err)
		}

		if jsonMode {
			fields := getFieldsFlag(cmd)
			var raw any
			if err := json.Unmarshal(data, &raw); err != nil {
				return failWithCode("parsing response: "+err.Error(), ExitError, output.E_SERVER)
			}
			// Try to parse as list of instances
			var instances []instanceResult
			if err := json.Unmarshal(data, &instances); err != nil {
				// Try DRF paginated response
				var page struct {
					Results []instanceResult `json:"results"`
				}
				if err2 := json.Unmarshal(data, &page); err2 == nil {
					instances = page.Results
				} else {
					// Return raw
					output.PrintJSON(raw)
					return nil
				}
			}
			items := make([]map[string]any, len(instances))
			for i, inst := range instances {
				items[i] = instanceResultToMap(inst)
			}
			if len(fields) > 0 {
				filtered := make([]map[string]any, len(items))
				for i, m := range items {
					filtered[i] = output.FilterMap(m, fields)
				}
				items = filtered
			}
			output.PrintJSON(items)
			return nil
		}

		var instances []instanceResult
		if err := json.Unmarshal(data, &instances); err != nil {
			output.Info("No instances found containing table.")
			return nil
		}
		if len(instances) == 0 {
			output.Info("No instances found containing table.")
			return nil
		}
		headers := []string{"ID", "NAME", "DB TYPE", "HOST", "PORT"}
		rows := make([][]string, len(instances))
		for i, inst := range instances {
			rows[i] = []string{
				inst.ID,
				inst.InstanceName,
				inst.DbType,
				inst.Host,
				strconv.Itoa(inst.Port),
			}
		}
		output.Table(headers, rows)
		return nil
	},
}

// ─── instance users ─────────────────────────────────────────────────────────

var instanceUsersCmd = &cobra.Command{
	Use:   "users",
	Short: "List database users on an instance",
	RunE: func(cmd *cobra.Command, args []string) error {
		instanceID, _ := cmd.Flags().GetInt("instance")
		if instanceID == 0 {
			return failArg("--instance is required")
		}
		saved, _ := cmd.Flags().GetBool("saved")

		client, _, _, err := newClient()
		if err != nil {
			return err
		}

		form := url.Values{
			"instance_id": {strconv.Itoa(instanceID)},
		}
		if saved {
			form.Set("saved", "true")
		}

		data, err := client.InternalPost(apiCtx(), "/instance/user/list", form)
		if err != nil {
			return handleAPIError(err)
		}

		if jsonMode {
			fields := getFieldsFlag(cmd)
			var raw any
			if err := json.Unmarshal(data, &raw); err != nil {
				return failWithCode("parsing response: "+err.Error(), ExitError, output.E_SERVER)
			}
			// Try to extract rows from internal response format
			var resp struct {
				Status int    `json:"status"`
				Msg    string `json:"msg"`
				Data   struct {
					Total int    `json:"total"`
					Rows  []map[string]any `json:"rows"`
				} `json:"data"`
			}
			if err := json.Unmarshal(data, &resp); err == nil && len(resp.Data.Rows) > 0 {
				items := resp.Data.Rows
				if len(fields) > 0 {
					filtered := make([]map[string]any, len(items))
					for i, m := range items {
						filtered[i] = output.FilterMap(m, fields)
					}
					items = filtered
				}
				output.PrintJSON(items)
				return nil
			}
			// Fallback: return raw
			if m, ok := raw.(map[string]any); ok && len(fields) > 0 {
				output.PrintJSON(output.FilterMap(m, fields))
			} else {
				output.PrintJSON(raw)
			}
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
		if err := json.Unmarshal(data, &resp); err != nil || len(resp.Data.Rows) == 0 {
			output.Info("No users found.")
			return nil
		}
		if len(resp.Data.Rows) == 0 {
			output.Info("No users found.")
			return nil
		}
		// Build table from dynamic keys
		var headers []string
		var rows [][]string
		for i, row := range resp.Data.Rows {
			if i == 0 {
				for k := range row {
					headers = append(headers, strings.ToUpper(k))
				}
			}
			var cells []string
			for _, h := range headers {
				key := strings.ToLower(h)
				if v, ok := row[key]; ok {
					cells = append(cells, fmt.Sprintf("%v", v))
				} else {
					cells = append(cells, "")
				}
			}
			rows = append(rows, cells)
		}
		if len(headers) > 0 {
			output.Table(headers, rows)
		}
		return nil
	},
}

// ─── helpers ────────────────────────────────────────────────────────────────

// instanceResult mirrors the Archery REST API instance JSON shape.
// ID is stored as a string per CLI-SPEC §8 (all IDs are strings).
type instanceResult struct {
	ID           string `json:"id"`
	InstanceName string `json:"instance_name"`
	DbType       string `json:"db_type"`
	Host         string `json:"host"`
	Port         int    `json:"port"`
	User         string `json:"user"`
	Password     string `json:"password"`
	DBName       string `json:"db_name"`
	Charset      string `json:"charset"`
	IsActive     bool   `json:"is_active"`
	Environment  string `json:"environment"`
	InstanceTag  string `json:"instance_tag"`
}

// UnmarshalJSON implements custom JSON unmarshaling to handle the Archery API
// returning numeric IDs while we store them as strings (CLI-SPEC §8).
func (r *instanceResult) UnmarshalJSON(data []byte) error {
	// Use an alias to avoid infinite recursion.
	type alias instanceResult
	var raw struct {
		alias
		ID json.Number `json:"id"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*r = instanceResult(raw.alias)
	r.ID = raw.ID.String()
	return nil
}

func instanceResultToMap(inst instanceResult) map[string]any {
	m := map[string]any{
		"id":           inst.ID,
		"instanceName": inst.InstanceName,
		"dbType":       inst.DbType,
		"host":         inst.Host,
		"port":         inst.Port,
		"user":         inst.User,
	}
	if inst.DBName != "" {
		m["dbName"] = inst.DBName
	}
	if inst.Charset != "" {
		m["charset"] = inst.Charset
	}
	if inst.Environment != "" {
		m["environment"] = inst.Environment
	}
	if inst.InstanceTag != "" {
		m["instanceTag"] = inst.InstanceTag
	}
	m["isActive"] = inst.IsActive
	return m
}

func parseInstanceID(s string) (string, error) {
	s = strings.TrimSpace(s)
	id, err := strconv.Atoi(s)
	if err != nil || id <= 0 {
		return "", failArg("INSTANCE_ID must be a positive integer")
	}
	return s, nil
}

func printInstanceField(label, value string) {
	if value != "" {
		fmt.Printf("  %-14s %s\n", output.FormatGray(label), value)
	}
}

func extractResourceItems(raw any) []map[string]any {
	if raw == nil {
		return nil
	}
	// Try direct []map
	if arr, ok := raw.([]any); ok {
		items := make([]map[string]any, 0, len(arr))
		for _, v := range arr {
			if m, ok := v.(map[string]any); ok {
				items = append(items, m)
			}
		}
		return items
	}
	// Try DRF paginated
	if m, ok := raw.(map[string]any); ok {
		if results, ok := m["results"]; ok {
			return extractResourceItems(results)
		}
		// Try internal paginated
		if data, ok := m["data"]; ok {
			if dm, ok := data.(map[string]any); ok {
				if rows, ok := dm["rows"]; ok {
					return extractResourceItems(rows)
				}
			}
		}
	}
	return nil
}

func printResourceTable(items []map[string]any) {
	if len(items) == 0 {
		output.Info("No resources found.")
		return
	}
	// Collect all keys from items
	keySet := map[string]bool{}
	for _, m := range items {
		for k := range m {
			keySet[k] = true
		}
	}
	var headers []string
	for k := range keySet {
		headers = append(headers, strings.ToUpper(k))
	}
	// Use stable order from first item
	if len(items) > 0 {
		headers = nil
		for k := range items[0] {
			headers = append(headers, strings.ToUpper(k))
		}
	}
	rows := make([][]string, len(items))
	for i, m := range items {
		cells := make([]string, len(headers))
		for j, h := range headers {
			key := strings.ToLower(h)
			// Try camelCase match
			for mk, mv := range m {
				if strings.ToLower(mk) == key {
					cells[j] = fmt.Sprintf("%v", mv)
					break
				}
			}
		}
		rows[i] = cells
	}
	output.Table(headers, rows)
}

func printDescribeTable(rows []map[string]any) {
	if len(rows) == 0 {
		output.Info("No column information found.")
		return
	}
	// Common DESCRIBE columns
	headers := []string{"FIELD", "TYPE", "NULL", "KEY", "DEFAULT", "EXTRA"}
	keyMap := map[string]string{
		"FIELD":   "field",
		"TYPE":    "type",
		"NULL":    "null",
		"KEY":     "key",
		"DEFAULT": "default",
		"EXTRA":   "extra",
	}
	tableRows := make([][]string, len(rows))
	for i, row := range rows {
		cells := make([]string, len(headers))
		for j, h := range headers {
			lower := keyMap[h]
			for k, v := range row {
				if strings.EqualFold(k, lower) {
					cells[j] = fmt.Sprintf("%v", v)
					break
				}
			}
		}
		tableRows[i] = cells
	}
	output.Table(headers, tableRows)
}
