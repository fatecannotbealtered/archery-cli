package cmd

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/fatecannotbealtered/archery-cli/internal/output"
	"github.com/spf13/cobra"
)

// instance import is a class-B batch onboard: it reads a CSV/JSON manifest and
// creates each instance via the single /v1/instance/ POST, looping client-side
// (Archery has no native bulk-create endpoint). It follows the shared batch
// contract (CLI-SPEC §15): one dry-run summary, one confirm token over the whole
// resolved set, per-item items[]/summary, and --continue-on-error. Partial
// failure does NOT roll back already-created instances.
var instanceImportCmd = &cobra.Command{
	Use:   "import",
	Short: "Batch-create instances from a CSV or JSON manifest file",
	RunE: func(cmd *cobra.Command, args []string) error {
		file, err := requireFlagString(cmd, "file", "--file")
		if err != nil {
			return err
		}
		format, _ := cmd.Flags().GetString("manifest-format")

		specs, parseErr := parseInstanceManifest(file, format)
		if parseErr != nil {
			return failArg(parseErr.Error())
		}
		if len(specs) == 0 {
			return failArg("manifest contains no instances")
		}

		// The natural key for each item is the instance name (§15.5).
		targets := make([]string, len(specs))
		changes := make([]map[string]any, len(specs))
		for i, s := range specs {
			targets[i] = s.Name
			changes[i] = map[string]any{
				"action":   "create",
				"resource": "instance",
				"name":     s.Name,
				"dbType":   s.DBType,
				"host":     s.Host,
				"port":     s.Port,
			}
		}

		if batchDryRunOrConfirm("import instances", targets, changes) {
			return nil
		}

		client, _, _, err := newClient()
		if err != nil {
			return err
		}

		continueOnError := continueOnErrorFlag(cmd, true)
		fields := getFieldsFlag(cmd)

		// Run by index so duplicate names in the manifest still map to the right spec.
		idx := -1
		items, summary := runBatch(targets, continueOnError, func(target string) (map[string]any, output.ErrorCode, bool, error) {
			idx++
			spec := specs[idx]
			payload, _, buildErr := spec.buildPayload()
			if buildErr != nil {
				return nil, output.E_VALIDATION, false, buildErr
			}
			inst, errMsg, code := createInstance(client, payload)
			if errMsg != "" {
				return nil, code, output.RetryableErrorCode(code), errBatch(errMsg)
			}
			m := instanceResultToMap(inst)
			if len(fields) > 0 {
				m = output.FilterMap(m, fields)
			}
			return m, "", false, nil
		})

		printBatchResult(items, summary)
		return nil
	},
}

// parseInstanceManifest loads instance specs from a CSV or JSON file. Format is
// inferred from the file extension when not given explicitly. The JSON shape is
// an array of objects; the CSV shape is a header row naming the columns. Both map
// onto instanceSpec via canonical column names.
func parseInstanceManifest(path, format string) ([]instanceSpec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading manifest: %w", err)
	}

	format = strings.ToLower(strings.TrimSpace(format))
	if format == "" {
		switch {
		case strings.HasSuffix(strings.ToLower(path), ".json"):
			format = "json"
		case strings.HasSuffix(strings.ToLower(path), ".csv"):
			format = "csv"
		default:
			return nil, fmt.Errorf("cannot infer format from %q; pass --format csv|json", path)
		}
	}

	switch format {
	case "json":
		return parseInstanceManifestJSON(data)
	case "csv":
		return parseInstanceManifestCSV(data)
	default:
		return nil, fmt.Errorf("--format must be 'csv' or 'json'")
	}
}

// manifestRow mirrors the manifest column/key names. Port accepts a JSON number
// or a CSV string, so it is parsed from a generic value.
type manifestRow struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	DBType   string `json:"db_type"`
	Host     string `json:"host"`
	Port     any    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	Mode     string `json:"mode"`
	DBName   string `json:"db_name"`
	Charset  string `json:"charset"`
}

func parseInstanceManifestJSON(data []byte) ([]instanceSpec, error) {
	var rows []manifestRow
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, fmt.Errorf("parsing JSON manifest (expected an array of instance objects): %w", err)
	}
	specs := make([]instanceSpec, 0, len(rows))
	for i, r := range rows {
		port, perr := manifestPort(r.Port)
		if perr != nil {
			return nil, fmt.Errorf("row %d (%s): %w", i+1, r.Name, perr)
		}
		specs = append(specs, instanceSpec{
			Name: strings.TrimSpace(r.Name), Type: r.Type, DBType: r.DBType,
			Host: r.Host, Port: port, User: r.User, Password: r.Password,
			Mode: r.Mode, DBName: r.DBName, Charset: r.Charset,
		})
	}
	return specs, nil
}

func parseInstanceManifestCSV(data []byte) ([]instanceSpec, error) {
	reader := csv.NewReader(strings.NewReader(string(data)))
	reader.TrimLeadingSpace = true
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("parsing CSV manifest: %w", err)
	}
	if len(records) < 2 {
		return nil, fmt.Errorf("CSV manifest needs a header row plus at least one data row")
	}

	// Map header names (canonicalized) to column indices.
	header := records[0]
	col := map[string]int{}
	for i, h := range header {
		col[canonicalManifestKey(h)] = i
	}

	get := func(row []string, key string) string {
		if i, ok := col[key]; ok && i < len(row) {
			return strings.TrimSpace(row[i])
		}
		return ""
	}

	specs := make([]instanceSpec, 0, len(records)-1)
	for i, row := range records[1:] {
		portStr := get(row, "port")
		port := 0
		if portStr != "" {
			p, perr := strconv.Atoi(portStr)
			if perr != nil {
				return nil, fmt.Errorf("row %d: port %q is not a number", i+1, portStr)
			}
			port = p
		}
		specs = append(specs, instanceSpec{
			Name: get(row, "name"), Type: get(row, "type"), DBType: get(row, "db_type"),
			Host: get(row, "host"), Port: port, User: get(row, "user"),
			Password: get(row, "password"), Mode: get(row, "mode"),
			DBName: get(row, "db_name"), Charset: get(row, "charset"),
		})
	}
	return specs, nil
}

// canonicalManifestKey normalizes a manifest column/key name to its internal key,
// accepting common spellings (dbType/db-type/db_type → db_type).
func canonicalManifestKey(k string) string {
	k = strings.ToLower(strings.TrimSpace(k))
	k = strings.ReplaceAll(k, "-", "_")
	switch k {
	case "dbtype":
		return "db_type"
	case "dbname", "database", "db":
		return "db_name"
	case "instance_name", "instancename":
		return "name"
	default:
		return k
	}
}

// manifestPort coerces a JSON port (number or string) to an int.
func manifestPort(v any) (int, error) {
	switch x := v.(type) {
	case nil:
		return 0, nil
	case float64:
		return int(x), nil
	case json.Number:
		n, err := x.Int64()
		return int(n), err
	case string:
		s := strings.TrimSpace(x)
		if s == "" {
			return 0, nil
		}
		return strconv.Atoi(s)
	default:
		return 0, fmt.Errorf("port %v is not a number", v)
	}
}
