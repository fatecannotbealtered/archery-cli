package cmd

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

func normalizeAgentValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		return normalizeAgentMap(x)
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = normalizeAgentValue(item)
		}
		return out
	case []map[string]any:
		out := make([]map[string]any, len(x))
		for i, item := range x {
			out[i] = normalizeAgentMap(item)
		}
		return out
	default:
		return v
	}
}

func normalizeAgentMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m)+1)
	for k, v := range m {
		if isIDField(k) {
			if s, ok := idValueString(v); ok {
				out[k] = s
				continue
			}
		}
		if isTimeField(k) {
			if s, ok := timeValueUTCString(v); ok {
				out[k] = s
				continue
			}
		}
		out[k] = normalizeAgentValue(v)
	}
	tagCommonUntrusted(out)
	return out
}

func normalizeAgentRows(items []map[string]any) []map[string]any {
	if items == nil {
		return nil
	}
	out := make([]map[string]any, len(items))
	for i, item := range items {
		out[i] = normalizeAgentMap(item)
	}
	return out
}

func isIDField(name string) bool {
	if name == "id" || strings.HasSuffix(name, "Id") || strings.HasSuffix(name, "ID") {
		return true
	}
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, "_id") || lower == "workflowid" || lower == "instanceid" || lower == "archiveid" || lower == "logid"
}

func idValueString(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case json.Number:
		return x.String(), true
	case int:
		return strconv.Itoa(x), true
	case int64:
		return strconv.FormatInt(x, 10), true
	case int32:
		return strconv.FormatInt(int64(x), 10), true
	case float64:
		if x == float64(int64(x)) {
			return strconv.FormatInt(int64(x), 10), true
		}
		return fmt.Sprintf("%v", x), true
	case float32:
		f := float64(x)
		if f == float64(int64(f)) {
			return strconv.FormatInt(int64(f), 10), true
		}
		return fmt.Sprintf("%v", x), true
	default:
		return "", false
	}
}

func isTimeField(name string) bool {
	lower := strings.ToLower(name)
	switch lower {
	case "created", "updated", "timestamp", "last_seen", "lastseen", "exec_time", "create_time", "start_time", "end_time", "start", "end":
		return true
	}
	return strings.HasSuffix(lower, "_time") ||
		strings.HasSuffix(lower, "time") ||
		strings.HasSuffix(lower, "_date") ||
		strings.HasSuffix(lower, "date") ||
		strings.HasSuffix(lower, "_at") ||
		strings.HasSuffix(lower, "at")
}

func timeValueUTCString(v any) (string, bool) {
	s, ok := v.(string)
	if !ok {
		return "", false
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return "", false
	}
	for _, layout := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05.000-0700",
		"2006-01-02T15:04:05-0700",
		"2006-01-02T15:04:05-07:00",
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
		"2006-01-02",
	} {
		parsed, err := parseAgentTime(layout, s)
		if err == nil {
			return parsed.UTC().Format(time.RFC3339), true
		}
	}
	return "", false
}

func parseAgentTime(layout, value string) (time.Time, error) {
	if layout == time.RFC3339 || layout == time.RFC3339Nano || strings.Contains(layout, "-07") || strings.Contains(layout, "Z07") {
		return time.Parse(layout, value)
	}
	return time.ParseInLocation(layout, value, time.UTC)
}

func tagCommonUntrusted(m map[string]any) {
	fields := existingUntrustedFields(m)
	addUntrustedFields(
		&fields,
		m,
		"title",
		"name",
		"comment",
		"comments",
		"description",
		"desc",
		"sql",
		"sql_text",
		"sqllog",
		"condition",
		"content",
		"html",
		"ddl",
		"definition",
		"body",
		"rows",
		"result",
		"info",
		"Info",
		"trx_query",
	)
	if len(fields) > 0 {
		m["_untrusted"] = fields
	}
}

func existingUntrustedFields(m map[string]any) []string {
	raw, ok := m["_untrusted"]
	if !ok {
		return nil
	}
	switch x := raw.(type) {
	case []string:
		return append([]string(nil), x...)
	case []any:
		out := []string{}
		for _, item := range x {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func addUntrustedFields(fields *[]string, m map[string]any, candidates ...string) {
	seen := map[string]bool{}
	for _, field := range *fields {
		seen[field] = true
	}
	for _, field := range candidates {
		if _, ok := m[field]; !ok || seen[field] {
			continue
		}
		*fields = append(*fields, field)
		seen[field] = true
	}
}
