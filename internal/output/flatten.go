package output

import "strings"

// FilterMap returns a copy of m containing only the keys requested in fieldNames.
// Lookup is CASE-INSENSITIVE (so --fields webUrl, weburl, WEBURL all work) but
// the result preserves the canonical key from m (so output JSON always uses the
// same key the schema documents).
func FilterMap(m map[string]any, fieldNames []string) map[string]any {
	if len(fieldNames) == 0 {
		return m
	}
	index := make(map[string]string, len(m))
	for k := range m {
		index[strings.ToLower(k)] = k
	}
	result := make(map[string]any, len(fieldNames))
	for _, name := range fieldNames {
		wanted := strings.TrimSpace(strings.ToLower(name))
		if origKey, ok := index[wanted]; ok {
			result[origKey] = m[origKey]
		}
	}
	if untrusted := filteredUntrustedFields(m, result); len(untrusted) > 0 {
		result["_untrusted"] = untrusted
	}
	return result
}

func filteredUntrustedFields(source, result map[string]any) []string {
	raw, ok := source["_untrusted"]
	if !ok {
		return nil
	}

	resultIndex := make(map[string]string, len(result))
	for k := range result {
		resultIndex[strings.ToLower(k)] = k
	}

	var sourceFields []string
	switch x := raw.(type) {
	case []string:
		sourceFields = x
	case []any:
		for _, item := range x {
			if s, ok := item.(string); ok {
				sourceFields = append(sourceFields, s)
			}
		}
	default:
		return nil
	}

	out := []string{}
	seen := map[string]bool{}
	for _, field := range sourceFields {
		if field == "" {
			continue
		}
		if canonical, ok := resultIndex[strings.ToLower(field)]; ok && !seen[canonical] {
			out = append(out, canonical)
			seen[canonical] = true
		}
	}
	return out
}

// ListMeta describes pagination for list command JSON output.
type ListMeta struct {
	Count   int  `json:"count"`
	Limit   int  `json:"limit,omitempty"`
	Page    int  `json:"page,omitempty"`
	Total   int  `json:"total,omitempty"`
	HasMore bool `json:"has_more"`
	All     bool `json:"all,omitempty"`
}

// ListEnvelope is the standard JSON shape for list commands.
type ListEnvelope struct {
	Items []map[string]any `json:"items"`
	ListMeta
}

// NewListEnvelope builds a list response with projected items.
func NewListEnvelope(items []map[string]any, meta ListMeta) ListEnvelope {
	if items == nil {
		items = []map[string]any{}
	}
	return ListEnvelope{Items: items, ListMeta: meta}
}

// ─── Workflow ───────────────────────────────────────────────────────────────

// FlatWorkflow is a token-efficient representation of an Archery workflow.
type FlatWorkflow struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Status      string `json:"status,omitempty"`
	WebURL      string `json:"webUrl,omitempty"`
}

// WorkflowToMap converts a FlatWorkflow to a map for field filtering.
func WorkflowToMap(f FlatWorkflow) map[string]any {
	m := map[string]any{
		"id":   f.ID,
		"name": f.Name,
	}
	if f.Description != "" {
		m["description"] = f.Description
	}
	if f.Status != "" {
		m["status"] = f.Status
	}
	if f.WebURL != "" {
		m["webUrl"] = f.WebURL
	}
	return m
}

// ─── Instance ───────────────────────────────────────────────────────────────

// FlatInstance is a token-efficient representation of an Archery instance.
type FlatInstance struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Type   string `json:"type,omitempty"`
	Host   string `json:"host,omitempty"`
	Port   int    `json:"port,omitempty"`
	User   string `json:"user,omitempty"`
	WebURL string `json:"webUrl,omitempty"`
}

// InstanceToMap converts a FlatInstance to a map for field filtering.
func InstanceToMap(f FlatInstance) map[string]any {
	m := map[string]any{
		"id":   f.ID,
		"name": f.Name,
	}
	if f.Type != "" {
		m["type"] = f.Type
	}
	if f.Host != "" {
		m["host"] = f.Host
	}
	if f.Port != 0 {
		m["port"] = f.Port
	}
	if f.User != "" {
		m["user"] = f.User
	}
	if f.WebURL != "" {
		m["webUrl"] = f.WebURL
	}
	return m
}

// ─── QueryResult ────────────────────────────────────────────────────────────

// FlatQueryResult is a token-efficient representation of an Archery query result.
type FlatQueryResult struct {
	ID        int    `json:"id"`
	SQL       string `json:"sql"`
	Instance  string `json:"instance,omitempty"`
	DBName    string `json:"dbName,omitempty"`
	Status    string `json:"status,omitempty"`
	EffectRow int    `json:"effectRow,omitempty"`
	Elapsed   string `json:"elapsed,omitempty"`
}

// QueryResultToMap converts a FlatQueryResult to a map for field filtering.
func QueryResultToMap(f FlatQueryResult) map[string]any {
	m := map[string]any{
		"id":  f.ID,
		"sql": f.SQL,
	}
	if f.Instance != "" {
		m["instance"] = f.Instance
	}
	if f.DBName != "" {
		m["dbName"] = f.DBName
	}
	if f.Status != "" {
		m["status"] = f.Status
	}
	if f.EffectRow != 0 {
		m["effectRow"] = f.EffectRow
	}
	if f.Elapsed != "" {
		m["elapsed"] = f.Elapsed
	}
	return m
}

// ─── SQLAudit ───────────────────────────────────────────────────────────────

// FlatSQLAudit is a token-efficient representation of an SQL audit result.
type FlatSQLAudit struct {
	ID          int    `json:"id"`
	SQL         string `json:"sql"`
	Level       string `json:"level,omitempty"`
	Message     string `json:"message,omitempty"`
	Stage       string `json:"stage,omitempty"`
	AffectedRow int    `json:"affectedRow,omitempty"`
}

// SQLAuditToMap converts a FlatSQLAudit to a map for field filtering.
func SQLAuditToMap(f FlatSQLAudit) map[string]any {
	m := map[string]any{
		"id":  f.ID,
		"sql": f.SQL,
	}
	if f.Level != "" {
		m["level"] = f.Level
	}
	if f.Message != "" {
		m["message"] = f.Message
	}
	if f.Stage != "" {
		m["stage"] = f.Stage
	}
	if f.AffectedRow != 0 {
		m["affectedRow"] = f.AffectedRow
	}
	return m
}

// ─── Permission ─────────────────────────────────────────────────────────────

// FlatPermission is a token-efficient representation of an Archery permission.
type FlatPermission struct {
	ID         int    `json:"id"`
	Username   string `json:"username"`
	Instance   string `json:"instance,omitempty"`
	DBName     string `json:"dbName,omitempty"`
	Permission string `json:"permission,omitempty"`
}

// PermissionToMap converts a FlatPermission to a map for field filtering.
func PermissionToMap(f FlatPermission) map[string]any {
	m := map[string]any{
		"id":       f.ID,
		"username": f.Username,
	}
	if f.Instance != "" {
		m["instance"] = f.Instance
	}
	if f.DBName != "" {
		m["dbName"] = f.DBName
	}
	if f.Permission != "" {
		m["permission"] = f.Permission
	}
	return m
}
