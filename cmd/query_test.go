package cmd

import "testing"

func TestQueryRunFlags(t *testing.T) {
	cmd := queryRunCmd

	type flagTest struct {
		name      string
		flagName  string
		wantType  string
		checkFunc func(t *testing.T)
	}

	tests := []flagTest{
		{
			name:     "instance",
			flagName: "instance",
			wantType: "string",
			checkFunc: func(t *testing.T) {
				got, _ := cmd.Flags().GetString("instance")
				if got != "" {
					t.Errorf("default = %q, want ''", got)
				}
			},
		},
		{
			name:     "db",
			flagName: "db",
			wantType: "string",
			checkFunc: func(t *testing.T) {
				got, _ := cmd.Flags().GetString("db")
				if got != "" {
					t.Errorf("default = %q, want ''", got)
				}
			},
		},
		{
			name:     "sql",
			flagName: "sql",
			wantType: "string",
			checkFunc: func(t *testing.T) {
				got, _ := cmd.Flags().GetString("sql")
				if got != "" {
					t.Errorf("default = %q, want ''", got)
				}
			},
		},
		{
			name:     "limit",
			flagName: "limit",
			wantType: "int",
			checkFunc: func(t *testing.T) {
				got, _ := cmd.Flags().GetInt("limit")
				if got != 0 {
					t.Errorf("default = %d, want 0", got)
				}
			},
		},
		{
			name:     "table",
			flagName: "table",
			wantType: "string",
			checkFunc: func(t *testing.T) {
				got, _ := cmd.Flags().GetString("table")
				if got != "" {
					t.Errorf("default = %q, want ''", got)
				}
			},
		},
		{
			name:     "schema",
			flagName: "schema",
			wantType: "string",
			checkFunc: func(t *testing.T) {
				got, _ := cmd.Flags().GetString("schema")
				if got != "" {
					t.Errorf("default = %q, want ''", got)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := cmd.Flags().Lookup(tt.flagName)
			if f == nil {
				t.Fatalf("flag %q not registered", tt.flagName)
			}
			if f.Value.Type() != tt.wantType {
				t.Errorf("flag %q type = %q, want %q", tt.flagName, f.Value.Type(), tt.wantType)
			}
			if tt.checkFunc != nil {
				tt.checkFunc(t)
			}
		})
	}
}

func TestQueryExplainFlags(t *testing.T) {
	cmd := queryExplainCmd

	type flagTest struct {
		name      string
		flagName  string
		wantType  string
		checkFunc func(t *testing.T)
	}

	tests := []flagTest{
		{
			name:     "instance",
			flagName: "instance",
			wantType: "string",
			checkFunc: func(t *testing.T) {
				got, _ := cmd.Flags().GetString("instance")
				if got != "" {
					t.Errorf("default = %q, want ''", got)
				}
			},
		},
		{
			name:     "db",
			flagName: "db",
			wantType: "string",
			checkFunc: func(t *testing.T) {
				got, _ := cmd.Flags().GetString("db")
				if got != "" {
					t.Errorf("default = %q, want ''", got)
				}
			},
		},
		{
			name:     "sql",
			flagName: "sql",
			wantType: "string",
			checkFunc: func(t *testing.T) {
				got, _ := cmd.Flags().GetString("sql")
				if got != "" {
					t.Errorf("default = %q, want ''", got)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := cmd.Flags().Lookup(tt.flagName)
			if f == nil {
				t.Fatalf("flag %q not registered", tt.flagName)
			}
			if f.Value.Type() != tt.wantType {
				t.Errorf("flag %q type = %q, want %q", tt.flagName, f.Value.Type(), tt.wantType)
			}
			if tt.checkFunc != nil {
				tt.checkFunc(t)
			}
		})
	}
}
