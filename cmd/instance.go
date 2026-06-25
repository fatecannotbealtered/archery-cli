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
	markRiskLevel(instanceUpdateCmd, "high")

	// instance delete
	instanceCmd.AddCommand(instanceDeleteCmd)
	markWrite(instanceDeleteCmd)
	markConfirm(instanceDeleteCmd)
	markRiskLevel(instanceDeleteCmd, "high")

	// instance import (batch onboard from a CSV/JSON manifest)
	instanceImportCmd.Flags().String("file", "", "Path to a CSV or JSON manifest of instances (required)")
	instanceImportCmd.Flags().String("manifest-format", "", "Manifest format: csv|json (default: inferred from file extension)")
	instanceImportCmd.Flags().Bool("continue-on-error", true, "Keep importing after an instance fails (default true)")
	instanceCmd.AddCommand(instanceImportCmd)
	markWrite(instanceImportCmd)
	markConfirm(instanceImportCmd)
	markRiskLevel(instanceImportCmd, "high")

	// instance test-instance (connectivity probe)
	instanceTestCmd.Flags().Int("instance", 0, "Instance ID (required)")
	instanceCmd.AddCommand(instanceTestCmd)

	// instance users
	instanceUsersCmd.Flags().Int("instance", 0, "Instance ID (required)")
	instanceUsersCmd.Flags().Bool("saved", false, "Filter by saved users only")
	instanceCmd.AddCommand(instanceUsersCmd)

	// instance create-db
	instanceCreateDBCmd.Flags().Int("instance", 0, "Instance ID (required)")
	instanceCreateDBCmd.Flags().String("db", "", "Database name to create (required)")
	instanceCreateDBCmd.Flags().String("owner", "", "Owner username")
	instanceCreateDBCmd.Flags().String("remark", "", "Remark")
	instanceCmd.AddCommand(instanceCreateDBCmd)
	markWrite(instanceCreateDBCmd)
	markConfirm(instanceCreateDBCmd)
	markRiskLevel(instanceCreateDBCmd, "high")

	// instance create-user
	instanceCreateUserCmd.Flags().Int("instance", 0, "Instance ID (required)")
	instanceCreateUserCmd.Flags().String("user", "", "Database username (required)")
	instanceCreateUserCmd.Flags().String("host", "", "Host pattern, e.g. % or 10.0.0.1 (required)")
	instanceCreateUserCmd.Flags().String("password", "", "Account password (required)")
	instanceCreateUserCmd.Flags().String("remark", "", "Remark")
	instanceCmd.AddCommand(instanceCreateUserCmd)
	markWrite(instanceCreateUserCmd)
	markConfirm(instanceCreateUserCmd)
	markRiskLevel(instanceCreateUserCmd, "high")

	// instance grant
	instanceGrantCmd.Flags().Int("instance", 0, "Instance ID (required)")
	instanceGrantCmd.Flags().String("user-host", "", "Target account as `user`@`host` (required)")
	instanceGrantCmd.Flags().String("op", "grant", "Operation: grant|revoke")
	instanceGrantCmd.Flags().String("level", "", "Privilege level: global|db|table|column (required)")
	instanceGrantCmd.Flags().String("privs", "", "Comma-separated privileges, e.g. SELECT,INSERT (required)")
	instanceGrantCmd.Flags().StringSlice("db", nil, "Database name(s); repeatable for db level")
	instanceGrantCmd.Flags().StringSlice("table", nil, "Table name(s); repeatable for table level")
	instanceGrantCmd.Flags().StringSlice("column", nil, "Column name(s); repeatable for column level")
	instanceCmd.AddCommand(instanceGrantCmd)
	markWrite(instanceGrantCmd)
	markConfirm(instanceGrantCmd)
	markRiskLevel(instanceGrantCmd, "high")
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

		var instances []instanceResult
		var total int
		var hasMore bool

		if client.Mode() == api.ModeSession {
			instances, total, err = listInstancesSession(client, instanceType, dbType, search, limit, offset)
		} else {
			instances, total, hasMore, err = listInstancesREST(client, instanceType, dbType, search, limit, offset)
		}
		if err != nil {
			return handleAPIError(err)
		}
		if total > offset+len(instances) {
			hasMore = true
		}

		if jsonMode {
			fields := getFieldsFlag(cmd)
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
			output.PrintJSON(output.NewListEnvelope(items, output.ListMeta{
				Count:   len(instances),
				Limit:   limit,
				Total:   total,
				HasMore: hasMore,
			}))
			return nil
		}

		if len(instances) == 0 {
			output.Info("No instances found.")
			return nil
		}
		headers := []string{"ID", "NAME", "DB TYPE", "HOST", "PORT", "USER"}
		rows := make([][]string, len(instances))
		for i, inst := range instances {
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

// listInstancesSession lists instances via the Django AJAX endpoint
// (POST /instance/list/), the default transport. The view casts limit/offset
// with int() and type/sortName/sortOrder with str(), so all four must be sent
// or it raises. type is only forwarded when the caller filters by master/slave;
// sending "all" would filter for a literal type='all' and return nothing.
func listInstancesSession(client *api.Client, instanceType, dbType, search string, limit, offset int) ([]instanceResult, int, error) {
	form := url.Values{
		"limit":     {strconv.Itoa(limit)},
		"offset":    {strconv.Itoa(offset)},
		"sortName":  {"instance_name"},
		"sortOrder": {"asc"},
	}
	if instanceType != "" {
		form.Set("type", instanceType)
	}
	if dbType != "" {
		form.Set("db_type", dbType)
	}
	if search != "" {
		form.Set("search", search)
	}

	data, err := client.SessionPost(apiCtx(), "/instance/list/", form)
	if err != nil {
		// /instance/list/ is gated by the DBA permission `sql.menu_instance_list`.
		// An ordinary user who can still *query* instances (granted via a resource
		// group) gets 403 here, even though the web SQL-query page lists those
		// instances fine — it uses the ungated, user-scoped
		// /group/user_all_instances/ endpoint instead. Fall back to it so a
		// non-DBA account can still discover the instances it is authorized to use.
		if apiStatusForbidden(err) {
			return listUserAllInstancesSession(client, instanceType, dbType, search, limit, offset)
		}
		return nil, 0, err
	}

	var resp struct {
		Total int              `json:"total"`
		Rows  []instanceResult `json:"rows"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, 0, &api.APIError{ErrorMessages: []string{"parsing instance list: " + err.Error()}}
	}
	return resp.Rows, resp.Total, nil
}

// listUserAllInstancesSession is the non-DBA fallback for instance discovery. It
// reads GET /group/user_all_instances/ (resource_group.user_all_instances), the
// ungated endpoint the web query page uses, which returns only the instances the
// user is authorized to use and only the fields {id, type, db_type,
// instance_name}. Host/port/credentials are NOT exposed here (they require the
// DBA list), so those fields stay empty. The endpoint has no search/paging, so
// --search and --limit/--offset are applied client-side.
func listUserAllInstancesSession(client *api.Client, instanceType, dbType, search string, limit, offset int) ([]instanceResult, int, error) {
	params := url.Values{}
	if instanceType != "" {
		params.Set("type", instanceType)
	}
	if dbType != "" {
		// The view reads getlist('db_type[]'), so the param name carries the [].
		params.Set("db_type[]", dbType)
	}
	path := "/group/user_all_instances/"
	if enc := params.Encode(); enc != "" {
		path += "?" + enc
	}

	data, err := client.SessionGet(apiCtx(), path)
	if err != nil {
		return nil, 0, err
	}

	var resp struct {
		Status int    `json:"status"`
		Msg    string `json:"msg"`
		Data   []struct {
			ID           json.Number `json:"id"`
			Type         string      `json:"type"`
			DbType       string      `json:"db_type"`
			InstanceName string      `json:"instance_name"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, 0, &api.APIError{ErrorMessages: []string{"parsing user instances: " + err.Error()}}
	}

	all := make([]instanceResult, 0, len(resp.Data))
	for _, d := range resp.Data {
		if search != "" && !strings.Contains(strings.ToLower(d.InstanceName), strings.ToLower(search)) {
			continue
		}
		all = append(all, instanceResult{
			ID:           d.ID.String(),
			InstanceName: d.InstanceName,
			DbType:       d.DbType,
		})
	}
	total := len(all)

	output.Warn("listed via user-authorized instances (/group/user_all_instances/): host/port/credentials need DBA permission and are omitted")

	// Apply client-side paging to mirror the server-paged path.
	if offset >= total {
		return []instanceResult{}, total, nil
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return all[offset:end], total, nil
}

// apiStatusForbidden reports whether err is an API error with HTTP 403.
func apiStatusForbidden(err error) bool {
	var apiErr *api.APIError
	return asAPI(err, &apiErr) && apiErr.StatusCode == 403
}

// listInstancesREST lists instances via the DRF endpoint (GET /api/v1/instance/),
// used when --mode jwt is selected.
//
// Archery's REST API (settings.py REST_FRAMEWORK) is wired as PageNumberPagination
// (param `page`, default PAGE_SIZE 5) with DjangoFilterBackend only — no
// SearchFilter. So DRF LimitOffset params (`limit`/`offset`) and `search` are all
// ignored server-side: a naive single GET only ever returns page 1. We therefore
// paginate via `page`, stopping when the contract's `next` link is null, filter
// `search` client-side (DRF exposes no name-search param), and apply
// --limit/--offset client-side — mirroring listUserAllInstancesSession.
func listInstancesREST(client *api.Client, instanceType, dbType, search string, limit, offset int) ([]instanceResult, int, bool, error) {
	// maxPages backstops a misbehaving paginator so a server that never returns a
	// null `next` cannot loop forever (5 rows/page upstream → 5000-instance reach).
	const maxPages = 1000
	want := offset + limit // rows needed to fill the requested window (no-search fast path)

	var all []instanceResult
	serverTotal := 0
	truncated := false

	for page := 1; page <= maxPages; page++ {
		params := url.Values{}
		if instanceType != "" {
			params.Set("instance_type", instanceType)
		}
		if dbType != "" {
			params.Set("db_type", dbType)
		}
		params.Set("page", strconv.Itoa(page))

		data, err := client.Get(apiCtx(), client.APIPath("/v1/instance/")+"?"+params.Encode())
		if err != nil {
			return nil, 0, false, err
		}

		var pg struct {
			Count   int              `json:"count"`
			Next    string           `json:"next"`
			Results []instanceResult `json:"results"`
		}
		if err := json.Unmarshal(data, &pg); err != nil {
			return nil, 0, false, &api.APIError{ErrorMessages: []string{"parsing instance list: " + err.Error()}}
		}
		serverTotal = pg.Count
		for _, r := range pg.Results {
			if search != "" && !strings.Contains(strings.ToLower(r.InstanceName), strings.ToLower(search)) {
				continue
			}
			all = append(all, r)
		}

		if pg.Next == "" {
			break // last page
		}
		// Fast path: with no client-side search filter the server `count` is the
		// true total, so stop once we have enough rows to fill the window.
		if search == "" && len(all) >= want {
			break
		}
		if page == maxPages {
			truncated = true
		}
	}

	if truncated {
		output.Warn("instance list hit the pagination safety cap; some instances may be omitted")
	}

	// total: server count is pre-filter, so when --search filtered client-side the
	// real total is the matched count.
	total := serverTotal
	if search != "" {
		total = len(all)
	}

	if offset >= len(all) {
		return []instanceResult{}, total, false, nil
	}
	end := offset + limit
	if end > len(all) {
		end = len(all)
	}
	pageRows := all[offset:end]
	return pageRows, total, offset+len(pageRows) < total, nil
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

		var inst instanceResult
		if client.Mode() == api.ModeSession {
			inst, err = detailInstanceSession(client, id)
		} else {
			inst, err = detailInstanceREST(client, id)
		}
		if err != nil {
			return handleAPIError(err)
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

// detailInstanceREST fetches one instance via GET /api/v1/instance/<id>/.
func detailInstanceREST(client *api.Client, id string) (instanceResult, error) {
	data, err := client.Get(apiCtx(), client.APIPath(fmt.Sprintf("/v1/instance/%s/", id)))
	if err != nil {
		return instanceResult{}, err
	}
	var inst instanceResult
	if err := json.Unmarshal(data, &inst); err != nil {
		return instanceResult{}, &api.APIError{ErrorMessages: []string{"parsing instance detail: " + err.Error()}}
	}
	return inst, nil
}

// detailInstanceSession resolves one instance by id from the AJAX list endpoint:
// v1.8.5 exposes no per-id detail JSON to session callers, so we page through the
// list and match locally. A page of 500 covers any realistic single-region fleet.
func detailInstanceSession(client *api.Client, id string) (instanceResult, error) {
	instances, _, err := listInstancesSession(client, "", "", "", 500, 0)
	if err != nil {
		return instanceResult{}, err
	}
	for _, inst := range instances {
		if inst.ID == id {
			return inst, nil
		}
	}
	return instanceResult{}, &api.APIError{StatusCode: 404, ErrorMessages: []string{"instance " + id + " not found"}}
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

		var items []map[string]any
		if client.Mode() == api.ModeSession {
			items, err = resourceSession(client, instanceID, resourceType, dbName, schemaName, tableName)
		} else {
			items, err = resourceREST(client, instanceID, resourceType, dbName, schemaName, tableName)
		}
		if err != nil {
			return handleAPIError(err)
		}

		if jsonMode {
			fields := getFieldsFlag(cmd)
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
		if len(items) == 0 {
			output.Info("No resources found.")
			return nil
		}
		printResourceTable(items)
		return nil
	},
}

// resourceSession lists databases/schemas/tables/columns via the Django AJAX
// endpoint (GET /instance/instance_resource/). The view reads query params and
// uses tb_name (not table_name); data is resource.rows — a flat list of names
// for database/schema/table, or richer rows for column. normalizeResourceRows
// folds both shapes into []map for the uniform resource_item_list schema.
func resourceSession(client *api.Client, instanceID int, resourceType, dbName, schemaName, tableName string) ([]map[string]any, error) {
	q := url.Values{
		"instance_id":   {strconv.Itoa(instanceID)},
		"resource_type": {resourceType},
	}
	if dbName != "" {
		q.Set("db_name", dbName)
	}
	if schemaName != "" {
		q.Set("schema_name", schemaName)
	}
	if tableName != "" {
		q.Set("tb_name", tableName)
	}
	data, err := client.SessionGet(apiCtx(), "/instance/instance_resource/?"+q.Encode())
	if err != nil {
		return nil, err
	}
	var resp struct {
		Status int             `json:"status"`
		Msg    string          `json:"msg"`
		Data   json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, &api.APIError{ErrorMessages: []string{"parsing resource list: " + err.Error()}}
	}
	if resp.Status != 0 {
		return nil, &api.APIError{ErrorMessages: []string{resp.Msg}}
	}
	return normalizeResourceRows(resp.Data), nil
}

// resourceREST lists resources via POST /api/v1/instance/resource/ (JWT mode).
func resourceREST(client *api.Client, instanceID int, resourceType, dbName, schemaName, tableName string) ([]map[string]any, error) {
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
	// The engine signature uses tb_name; mirror it for the REST serializer.
	if tableName != "" {
		payload["tb_name"] = tableName
	}
	data, err := client.Post(apiCtx(), client.APIPath("/v1/instance/resource/"), payload)
	if err != nil {
		return nil, err
	}
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, &api.APIError{ErrorMessages: []string{"parsing resource list: " + err.Error()}}
	}
	return extractResourceItems(raw), nil
}

// normalizeResourceRows turns the engine's resource.rows into []map. Scalars
// (database/schema/table names) become {"name": value}; positional arrays are
// indexed (col0/col1/...); maps pass through normalized.
func normalizeResourceRows(data json.RawMessage) []map[string]any {
	if len(data) == 0 {
		return nil
	}
	var arr []any
	if err := json.Unmarshal(data, &arr); err != nil {
		return nil
	}
	items := make([]map[string]any, 0, len(arr))
	for _, v := range arr {
		switch row := v.(type) {
		case map[string]any:
			items = append(items, normalizeAgentMap(row))
		case []any:
			m := map[string]any{}
			for i, cell := range row {
				m[fmt.Sprintf("col%d", i)] = cell
			}
			items = append(items, m)
		default:
			items = append(items, map[string]any{"name": row})
		}
	}
	return items
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

		// describe is session-only in v1.8.5 (no REST route). It reads tb_name
		// (not table_name) and returns a ResultSet {column_list, rows} where rows
		// are positional arrays. Zip them into per-row maps for a stable shape.
		form := url.Values{
			"instance_name": {instanceName},
			"db_name":       {dbName},
			"tb_name":       {tableName},
		}
		if schemaName != "" {
			form.Set("schema_name", schemaName)
		}

		data, err := client.SessionPost(apiCtx(), "/instance/describetable/", form)
		if err != nil {
			return handleAPIError(err)
		}

		columns, err := parseDescribeColumns(data)
		if err != nil {
			return handleAPIError(err)
		}

		if jsonMode {
			fields := getFieldsFlag(cmd)
			for _, col := range columns {
				api.TagUntrusted(col, "comment", "Comment", "Create Table")
			}
			m := map[string]any{
				"instance": instanceName,
				"db":       dbName,
				"table":    tableName,
				"columns":  columns,
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

		if len(columns) == 0 {
			output.Info("No column information found.")
			return nil
		}
		printDescribeTable(columns)
		return nil
	},
}

// parseDescribeColumns parses the describetable response. data is
// {status,msg,data:{column_list,rows,error}} where rows are positional arrays.
// Each row is zipped with column_list into a map; if column_list is empty the
// row is indexed col0/col1/.... A non-zero status surfaces as an APIError.
func parseDescribeColumns(data []byte) ([]map[string]any, error) {
	var resp struct {
		Status int    `json:"status"`
		Msg    string `json:"msg"`
		Data   struct {
			ColumnList []string `json:"column_list"`
			Rows       [][]any  `json:"rows"`
			Error      any      `json:"error"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, &api.APIError{ErrorMessages: []string{"parsing describe response: " + err.Error()}}
	}
	if resp.Status != 0 {
		msg := resp.Msg
		if msg == "" {
			msg = "describe failed"
		}
		return nil, &api.APIError{ErrorMessages: []string{msg}}
	}
	cols := resp.Data.ColumnList
	out := make([]map[string]any, 0, len(resp.Data.Rows))
	for _, row := range resp.Data.Rows {
		m := map[string]any{}
		for i, cell := range row {
			key := fmt.Sprintf("col%d", i)
			if i < len(cols) && cols[i] != "" {
				key = cols[i]
			}
			m[key] = cell
		}
		out = append(out, m)
	}
	return out, nil
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
		instanceType, _ := cmd.Flags().GetString("type")
		dbType, err := requireFlagString(cmd, "db-type", "--db-type")
		if err != nil {
			return err
		}
		host, err := requireFlagString(cmd, "host", "--host")
		if err != nil {
			return err
		}
		port, _ := cmd.Flags().GetInt("port")
		user, err := requireFlagString(cmd, "user", "--user")
		if err != nil {
			return err
		}
		password, _ := cmd.Flags().GetString("password")
		mode, _ := cmd.Flags().GetString("mode")
		dbName, _ := cmd.Flags().GetString("db")
		charset, _ := cmd.Flags().GetString("charset")

		spec := instanceSpec{
			Name: name, Type: instanceType, DBType: dbType, Host: host,
			Port: port, User: user, Password: password, Mode: mode,
			DBName: dbName, Charset: charset,
		}
		payload, detail, err := spec.buildPayload()
		if err != nil {
			return failArg(err.Error())
		}
		confirmPayload := map[string]any{
			"payload": payload,
		}
		if markDryRunOrConfirmWithPayload("create instance", detail, confirmPayload) {
			return nil
		}

		client, _, _, err := newClient()
		if err != nil {
			return err
		}

		inst, errMsg, code := createInstance(client, payload)
		if errMsg != "" {
			return failWithCode(errMsg, code)
		}

		if jsonMode {
			output.PrintJSON(instanceResultToMap(inst))
			return nil
		}
		output.Success(fmt.Sprintf("Instance %q created (ID: %s)", inst.InstanceName, inst.ID))
		return nil
	},
}

// instanceSpec is the validated input for creating one instance. It is the
// shared shape between `instance create` (flags) and `instance import` (manifest
// rows), so both produce an identical upstream payload.
type instanceSpec struct {
	Name     string
	Type     string
	DBType   string
	Host     string
	Port     int
	User     string
	Password string
	Mode     string
	DBName   string
	Charset  string
}

// buildPayload validates the spec and returns the upstream POST body plus a
// secret-masked detail map for the dry-run preview. Validation errors return
// failArg (exit 2) for the single-create path; the batch path wraps them per item.
func (s instanceSpec) buildPayload() (payload map[string]any, detail map[string]any, err error) {
	if s.Name == "" {
		return nil, nil, errBatch("instance name is required")
	}
	instanceType := strings.ToLower(s.Type)
	if instanceType != "master" && instanceType != "slave" {
		return nil, nil, errBatch("type must be 'master' or 'slave'")
	}
	if s.DBType == "" {
		return nil, nil, errBatch("db-type is required")
	}
	if s.Host == "" {
		return nil, nil, errBatch("host is required")
	}
	if s.Port <= 0 || s.Port > 65535 {
		return nil, nil, errBatch("port must be between 1 and 65535")
	}
	if s.User == "" {
		return nil, nil, errBatch("user is required")
	}

	payload = map[string]any{
		"instance_name": s.Name,
		// Archery's Instance.type is a CharField with choices
		// ('master','slave'); it must be the string, not a numeric enum.
		"type":    instanceType,
		"db_type": s.DBType,
		"host":    s.Host,
		"port":    s.Port,
		"user":    s.User,
	}
	detail = map[string]any{
		"name": s.Name, "type": instanceType, "dbType": s.DBType,
		"host": s.Host, "port": s.Port, "user": s.User,
	}
	if s.Password != "" {
		payload["password"] = s.Password
		detail["password"] = "***"
	}
	if s.Mode != "" {
		payload["mode"] = s.Mode
		detail["mode"] = s.Mode
	}
	if s.DBName != "" {
		payload["db_name"] = s.DBName
		detail["db"] = s.DBName
	}
	if s.Charset != "" {
		payload["charset"] = s.Charset
		detail["charset"] = s.Charset
	}
	return payload, detail, nil
}

// createInstance POSTs one instance payload and parses the result. Shared by the
// single create path and the import loop. Instance management is REST-only
// (admin scope); session web has no create route.
func createInstance(client *api.Client, payload map[string]any) (instanceResult, string, output.ErrorCode) {
	data, err := client.Post(apiCtx(), client.APIPath("/v1/instance/"), payload)
	if err != nil {
		return instanceResult{}, err.Error(), errorCodeForAPIErr(err)
	}
	var inst instanceResult
	if err := json.Unmarshal(data, &inst); err != nil {
		return instanceResult{}, "parsing response: " + err.Error(), output.E_SERVER
	}
	return inst, "", ""
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
		confirmPayload := map[string]any{
			"instanceId": id,
			"payload":    payload,
		}
		if markDryRunOrConfirmWithPayload("update instance", detail, confirmPayload) {
			return nil
		}

		client, _, _, err := newClient()
		if err != nil {
			return err
		}

		path := client.APIPath(fmt.Sprintf("/v1/instance/%s/", id))
		data, err := client.Put(apiCtx(), path, payload)
		if err != nil {
			return handleAPIError(err)
		}

		var inst instanceResult
		if err := json.Unmarshal(data, &inst); err != nil {
			return failWithCode("parsing response: "+err.Error(), output.E_SERVER)
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

		if markDryRunOrConfirm("delete instance", map[string]any{"instanceId": id}) {
			return nil
		}

		client, _, _, err := newClient()
		if err != nil {
			return err
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

// ─── instance test-instance ─────────────────────────────────────────────────

var instanceTestCmd = &cobra.Command{
	Use:   "test-instance",
	Short: "Test connectivity to an instance",
	RunE: func(cmd *cobra.Command, args []string) error {
		instanceID, _ := cmd.Flags().GetInt("instance")
		if instanceID == 0 {
			return failArg("--instance is required")
		}

		client, _, _, err := newClient()
		if err != nil {
			return err
		}

		// check.instance opens a connection and lists databases; status 0 = OK,
		// status 1 carries the connection error in msg. Session-only endpoint.
		form := url.Values{"instance_id": {strconv.Itoa(instanceID)}}
		data, err := client.SessionPost(apiCtx(), "/check/instance/", form)
		if err != nil {
			return handleAPIError(err)
		}

		var resp struct {
			Status int    `json:"status"`
			Msg    string `json:"msg"`
		}
		if err := json.Unmarshal(data, &resp); err != nil {
			return failWithCode("parsing response: "+err.Error(), output.E_SERVER)
		}

		reachable := resp.Status == 0
		result := map[string]any{
			"instanceId": strconv.Itoa(instanceID),
			"reachable":  reachable,
		}
		if !reachable {
			result["message"] = resp.Msg
			api.TagUntrusted(result, "message")
		}

		if jsonMode {
			output.PrintJSON(result)
			return nil
		}
		if reachable {
			output.Success(fmt.Sprintf("Instance %d is reachable", instanceID))
		} else {
			output.Error(fmt.Sprintf("Instance %d is NOT reachable: %s", instanceID, resp.Msg))
		}
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

		// users is session-only. NOTE: route has no trailing slash. The response
		// carries rows at the top level ({status,msg,rows}), not under data.
		data, err := client.SessionPost(apiCtx(), "/instance/user/list", form)
		if err != nil {
			return handleAPIError(err)
		}

		var resp struct {
			Status int              `json:"status"`
			Msg    string           `json:"msg"`
			Rows   []map[string]any `json:"rows"`
		}
		if err := json.Unmarshal(data, &resp); err != nil {
			return failWithCode("parsing response: "+err.Error(), output.E_SERVER)
		}
		if resp.Status != 0 {
			return failWithCode(resp.Msg, output.E_VALIDATION)
		}

		if jsonMode {
			fields := getFieldsFlag(cmd)
			items := normalizeAgentRows(resp.Rows)
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

		if len(resp.Rows) == 0 {
			output.Info("No users found.")
			return nil
		}
		printDynamicTable(resp.Rows)
		return nil
	},
}

// ─── instance create-db ─────────────────────────────────────────────────────

var instanceCreateDBCmd = &cobra.Command{
	Use:   "create-db",
	Short: "Create a database on an instance",
	RunE: func(cmd *cobra.Command, args []string) error {
		instanceID, _ := cmd.Flags().GetInt("instance")
		if instanceID == 0 {
			return failArg("--instance is required")
		}
		dbName, err := requireFlagString(cmd, "db", "--db")
		if err != nil {
			return err
		}
		owner, _ := cmd.Flags().GetString("owner")
		remark, _ := cmd.Flags().GetString("remark")

		detail := map[string]any{"instanceId": strconv.Itoa(instanceID), "db": dbName}
		if owner != "" {
			detail["owner"] = owner
		}
		if markDryRunOrConfirmWithPayload("create database", detail, detail) {
			return nil
		}

		client, _, _, err := newClient()
		if err != nil {
			return err
		}

		form := url.Values{
			"instance_id": {strconv.Itoa(instanceID)},
			"db_name":     {dbName},
		}
		if owner != "" {
			form.Set("owner", owner)
		}
		if remark != "" {
			form.Set("remark", remark)
		}

		if err := sessionMutate(client, "/instance/database/create/", form); err != nil {
			return handleAPIError(err)
		}

		if jsonMode {
			output.PrintJSON(map[string]any{
				"created":    true,
				"instanceId": strconv.Itoa(instanceID),
				"db":         dbName,
			})
			return nil
		}
		output.Success(fmt.Sprintf("Database %q created on instance %d", dbName, instanceID))
		return nil
	},
}

// ─── instance create-user ───────────────────────────────────────────────────

var instanceCreateUserCmd = &cobra.Command{
	Use:   "create-user",
	Short: "Create a database account on an instance",
	RunE: func(cmd *cobra.Command, args []string) error {
		instanceID, _ := cmd.Flags().GetInt("instance")
		if instanceID == 0 {
			return failArg("--instance is required")
		}
		user, err := requireFlagString(cmd, "user", "--user")
		if err != nil {
			return err
		}
		host, err := requireFlagString(cmd, "host", "--host")
		if err != nil {
			return err
		}
		password, err := requireFlagString(cmd, "password", "--password")
		if err != nil {
			return err
		}
		remark, _ := cmd.Flags().GetString("remark")

		detail := map[string]any{
			"instanceId": strconv.Itoa(instanceID),
			"user":       user,
			"host":       host,
			"password":   "***",
		}
		confirmPayload := map[string]any{
			"instanceId": strconv.Itoa(instanceID),
			"user":       user,
			"host":       host,
		}
		if markDryRunOrConfirmWithPayload("create database account", detail, confirmPayload) {
			return nil
		}

		client, _, _, err := newClient()
		if err != nil {
			return err
		}

		// instance_account.create requires password1==password2.
		form := url.Values{
			"instance_id": {strconv.Itoa(instanceID)},
			"user":        {user},
			"host":        {host},
			"password1":   {password},
			"password2":   {password},
		}
		if remark != "" {
			form.Set("remark", remark)
		}

		if err := sessionMutate(client, "/instance/user/create/", form); err != nil {
			return handleAPIError(err)
		}

		if jsonMode {
			output.PrintJSON(map[string]any{
				"created":    true,
				"instanceId": strconv.Itoa(instanceID),
				"user":       user,
				"host":       host,
			})
			return nil
		}
		output.Success(fmt.Sprintf("Account '%s'@'%s' created on instance %d", user, host, instanceID))
		return nil
	},
}

// ─── instance grant ─────────────────────────────────────────────────────────

var instanceGrantCmd = &cobra.Command{
	Use:   "grant",
	Short: "Grant or revoke privileges for a database account",
	RunE: func(cmd *cobra.Command, args []string) error {
		instanceID, _ := cmd.Flags().GetInt("instance")
		if instanceID == 0 {
			return failArg("--instance is required")
		}
		userHost, err := requireFlagString(cmd, "user-host", "--user-host")
		if err != nil {
			return err
		}
		// The grant view interpolates user_host straight into GRANT ... TO <x>, so
		// it must be the backtick-quoted `user`@`host` form. Accept the friendly
		// user@host and quote it; pass an already-quoted value through unchanged.
		sqlUserHost, err := quoteUserHost(userHost)
		if err != nil {
			return err
		}
		op, _ := cmd.Flags().GetString("op")
		opType, err := grantOpType(op)
		if err != nil {
			return err
		}
		level, _ := cmd.Flags().GetString("level")
		privType, privKey, err := grantPrivType(level)
		if err != nil {
			return err
		}
		privsRaw, _ := cmd.Flags().GetString("privs")
		privs := splitCSVNonEmpty(privsRaw)
		if len(privs) == 0 {
			return failArg("--privs is required (comma-separated privilege names)")
		}
		dbNames, _ := cmd.Flags().GetStringSlice("db")
		tbNames, _ := cmd.Flags().GetStringSlice("table")
		colNames, _ := cmd.Flags().GetStringSlice("column")

		// Validate the level-specific operands before any network call.
		switch level {
		case "db":
			if len(dbNames) == 0 {
				return failArg("--db is required at least once for db level")
			}
		case "table":
			if len(dbNames) != 1 {
				return failArg("table level needs exactly one --db")
			}
			if len(tbNames) == 0 {
				return failArg("--table is required at least once for table level")
			}
		case "column":
			if len(dbNames) != 1 {
				return failArg("column level needs exactly one --db")
			}
			if len(tbNames) != 1 {
				return failArg("column level needs exactly one --table")
			}
			if len(colNames) == 0 {
				return failArg("--column is required at least once for column level")
			}
		}

		privsJSON, err := json.Marshal(map[string][]string{privKey: privs})
		if err != nil {
			return failArg("encoding privs: " + err.Error())
		}

		detail := map[string]any{
			"instanceId": strconv.Itoa(instanceID),
			"userHost":   userHost,
			"op":         strings.ToLower(op),
			"level":      level,
			"privs":      privs,
		}
		if len(dbNames) > 0 {
			detail["db"] = dbNames
		}
		if len(tbNames) > 0 {
			detail["table"] = tbNames
		}
		if len(colNames) > 0 {
			detail["column"] = colNames
		}
		if markDryRunOrConfirmWithPayload("change account privileges", detail, detail) {
			return nil
		}

		client, _, _, err := newClient()
		if err != nil {
			return err
		}

		form := url.Values{
			"instance_id": {strconv.Itoa(instanceID)},
			"user_host":   {sqlUserHost},
			"op_type":     {strconv.Itoa(opType)},
			"priv_type":   {strconv.Itoa(privType)},
			"privs":       {string(privsJSON)},
		}
		// The view reads db_name[] (getlist) at db level, but a scalar db_name at
		// table/column level; tb_name[] at table level and a scalar tb_name at
		// column level (then col_name[]).
		switch level {
		case "db":
			for _, db := range dbNames {
				form.Add("db_name[]", db)
			}
		case "table":
			form.Set("db_name", dbNames[0])
			for _, tb := range tbNames {
				form.Add("tb_name[]", tb)
			}
		case "column":
			form.Set("db_name", dbNames[0])
			form.Set("tb_name", tbNames[0])
			for _, col := range colNames {
				form.Add("col_name[]", col)
			}
		}

		data, err := client.SessionPost(apiCtx(), "/instance/user/grant/", form)
		if err != nil {
			return handleAPIError(err)
		}
		var resp struct {
			Status int    `json:"status"`
			Msg    string `json:"msg"`
			Data   any    `json:"data"`
		}
		if err := json.Unmarshal(data, &resp); err != nil {
			return failWithCode("parsing response: "+err.Error(), output.E_SERVER)
		}
		if resp.Status != 0 {
			return failWithCode(resp.Msg, output.E_VALIDATION)
		}

		// data is the executed GRANT/REVOKE SQL (external content).
		grantSQL, _ := resp.Data.(string)
		if jsonMode {
			out := map[string]any{
				"applied":    true,
				"instanceId": strconv.Itoa(instanceID),
				"userHost":   userHost,
				"sql":        grantSQL,
			}
			api.TagUntrusted(out, "sql")
			output.PrintJSON(out)
			return nil
		}
		output.Success(fmt.Sprintf("Privileges changed for %s on instance %d", userHost, instanceID))
		return nil
	},
}

// quoteUserHost turns a friendly user@host into the backtick-quoted `user`@`host`
// the grant view splices into SQL. A value that already contains a backtick is
// assumed pre-quoted (e.g. copied from `instance users`) and passed through.
// The host defaults to % when omitted, matching MySQL's own default.
func quoteUserHost(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", failArg("--user-host is required as user@host")
	}
	if strings.Contains(s, "`") {
		return s, nil
	}
	at := strings.LastIndex(s, "@")
	var user, host string
	if at < 0 {
		user, host = s, "%"
	} else {
		user, host = s[:at], s[at+1:]
	}
	user = strings.TrimSpace(user)
	host = strings.TrimSpace(host)
	if user == "" {
		return "", failArg("--user-host must include a username, as user@host")
	}
	if host == "" {
		host = "%"
	}
	return fmt.Sprintf("`%s`@`%s`", user, host), nil
}

// grantOpType maps the --op flag to Archery's op_type int (0 grant, 1 revoke).
func grantOpType(op string) (int, error) {
	switch strings.ToLower(strings.TrimSpace(op)) {
	case "grant", "":
		return 0, nil
	case "revoke":
		return 1, nil
	default:
		return 0, failArg("--op must be 'grant' or 'revoke'")
	}
}

// grantPrivType maps the --level flag to Archery's priv_type int and the privs
// JSON key the view expects (global_privs/db_privs/tb_privs/col_privs).
func grantPrivType(level string) (int, string, error) {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "global":
		return 0, "global_privs", nil
	case "db":
		return 1, "db_privs", nil
	case "table":
		return 2, "tb_privs", nil
	case "column":
		return 3, "col_privs", nil
	default:
		return 0, "", failArg("--level must be one of: global, db, table, column")
	}
}

// sessionMutate POSTs a form to a session endpoint that answers with the Archery
// internal envelope {status,msg,data}, surfacing status!=0 as a validation error.
func sessionMutate(client *api.Client, path string, form url.Values) error {
	data, err := client.SessionPost(apiCtx(), path, form)
	if err != nil {
		return err
	}
	var resp struct {
		Status int    `json:"status"`
		Msg    string `json:"msg"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return &api.APIError{ErrorMessages: []string{"parsing response: " + err.Error()}}
	}
	if resp.Status != 0 {
		msg := resp.Msg
		if msg == "" {
			msg = "operation failed"
		}
		return &api.APIError{StatusCode: 400, ErrorMessages: []string{msg}}
	}
	return nil
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
	// Archery's instance_tag is a ManyToMany relation serialized as an array.
	InstanceTag []any `json:"instance_tag"`
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
	if len(inst.InstanceTag) > 0 {
		m["instanceTag"] = inst.InstanceTag
	}
	m["isActive"] = inst.IsActive
	tagCommonUntrusted(m)
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

// splitCSVNonEmpty splits a comma-separated string, trimming each element and
// dropping empties.
func splitCSVNonEmpty(s string) []string {
	out := []string{}
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func extractResourceItems(raw any) []map[string]any {
	if raw == nil {
		return nil
	}
	// Try direct []map
	if arr, ok := raw.([]any); ok {
		items := make([]map[string]any, 0, len(arr))
		for _, v := range arr {
			switch row := v.(type) {
			case map[string]any:
				items = append(items, normalizeAgentMap(row))
			default:
				items = append(items, map[string]any{"name": row})
			}
		}
		return items
	}
	// Try DRF paginated
	if m, ok := raw.(map[string]any); ok {
		// Archery's REST instance-resource endpoint (POST /api/v1/instance/resource/)
		// wraps the rows as {"count": N, "result": [...]} — note the singular
		// `result`. Without this the whole list fell through to nil, so `instance
		// resource` returned `data: null` in --mode jwt.
		if result, ok := m["result"]; ok {
			return extractResourceItems(result)
		}
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
			return extractResourceItems(data)
		}
	}
	return nil
}

func printResourceTable(items []map[string]any) {
	if len(items) == 0 {
		output.Info("No resources found.")
		return
	}
	printDynamicTable(items)
}

func printDescribeTable(rows []map[string]any) {
	if len(rows) == 0 {
		output.Info("No column information found.")
		return
	}
	printDynamicTable(rows)
}
