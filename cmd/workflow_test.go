package cmd

import "testing"

func TestWorkflowListFlags(t *testing.T) {
	cmd := workflowListCmd

	type flagTest struct {
		name      string
		flagName  string
		wantType  string
		checkFunc func(t *testing.T)
	}

	tests := []flagTest{
		{
			name:     "status",
			flagName: "status",
			wantType: "string",
			checkFunc: func(t *testing.T) {
				got, _ := cmd.Flags().GetString("status")
				if got != "" {
					t.Errorf("default = %q, want ''", got)
				}
			},
		},
		{
			name:     "engineer",
			flagName: "engineer",
			wantType: "string",
			checkFunc: func(t *testing.T) {
				got, _ := cmd.Flags().GetString("engineer")
				if got != "" {
					t.Errorf("default = %q, want ''", got)
				}
			},
		},
		{
			name:     "instance",
			flagName: "instance",
			wantType: "int",
			checkFunc: func(t *testing.T) {
				got, _ := cmd.Flags().GetInt("instance")
				if got != 0 {
					t.Errorf("default = %d, want 0", got)
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
			name:     "limit",
			flagName: "limit",
			wantType: "int",
			checkFunc: func(t *testing.T) {
				got, _ := cmd.Flags().GetInt("limit")
				if got != 20 {
					t.Errorf("default = %d, want 20", got)
				}
			},
		},
		{
			name:     "offset",
			flagName: "offset",
			wantType: "int",
			checkFunc: func(t *testing.T) {
				got, _ := cmd.Flags().GetInt("offset")
				if got != 0 {
					t.Errorf("default = %d, want 0", got)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := cmd.Flags().Lookup(tt.flagName)
			if f == nil {
				t.Fatalf("flag %q not registered", tt.flagName)
				return
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

func TestWorkflowSubmitFlags(t *testing.T) {
	cmd := workflowSubmitCmd

	type flagTest struct {
		name      string
		flagName  string
		wantType  string
		checkFunc func(t *testing.T)
	}

	tests := []flagTest{
		{
			name:     "name",
			flagName: "name",
			wantType: "string",
			checkFunc: func(t *testing.T) {
				got, _ := cmd.Flags().GetString("name")
				if got != "" {
					t.Errorf("default = %q, want ''", got)
				}
			},
		},
		{
			name:     "instance",
			flagName: "instance",
			wantType: "int",
			checkFunc: func(t *testing.T) {
				got, _ := cmd.Flags().GetInt("instance")
				if got != 0 {
					t.Errorf("default = %d, want 0", got)
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
			name:     "group",
			flagName: "group",
			wantType: "int",
			checkFunc: func(t *testing.T) {
				got, _ := cmd.Flags().GetInt("group")
				if got != 0 {
					t.Errorf("default = %d, want 0", got)
				}
			},
		},
		{
			name:     "backup",
			flagName: "backup",
			wantType: "bool",
			checkFunc: func(t *testing.T) {
				got, _ := cmd.Flags().GetBool("backup")
				if got != true {
					t.Errorf("default = %v, want true", got)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := cmd.Flags().Lookup(tt.flagName)
			if f == nil {
				t.Fatalf("flag %q not registered", tt.flagName)
				return
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

func TestWorkflowAuditFlags(t *testing.T) {
	cmd := workflowAuditCmd

	type flagTest struct {
		name      string
		flagName  string
		wantType  string
		checkFunc func(t *testing.T)
	}

	tests := []flagTest{
		{
			name:     "action",
			flagName: "action",
			wantType: "string",
			checkFunc: func(t *testing.T) {
				got, _ := cmd.Flags().GetString("action")
				if got != "" {
					t.Errorf("default = %q, want ''", got)
				}
			},
		},
		{
			name:     "remark",
			flagName: "remark",
			wantType: "string",
			checkFunc: func(t *testing.T) {
				got, _ := cmd.Flags().GetString("remark")
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
				return
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

func TestWorkflowExecuteFlags(t *testing.T) {
	cmd := workflowExecuteCmd

	f := cmd.Flags().Lookup("mode")
	if f == nil {
		t.Fatal("flag 'mode' not registered on workflow execute")
		return
	}
	if f.Value.Type() != "string" {
		t.Errorf("flag 'mode' type = %q, want 'string'", f.Value.Type())
	}
	got, err := cmd.Flags().GetString("mode")
	if err != nil {
		t.Fatalf("GetString('mode'): %v", err)
	}
	if got != "auto" {
		t.Errorf("flag 'mode' default = %q, want 'auto'", got)
	}
}

func TestWorkflowSubmitSessionFlags(t *testing.T) {
	cmd := workflowSubmitCmd
	for _, name := range []string{"instance-name", "group-name", "cc"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("flag %q not registered on workflow submit", name)
		}
	}
}

func TestWorkflowAuditListFlags(t *testing.T) {
	cmd := workflowAuditListCmd
	for _, name := range []string{"status", "instance", "group", "search", "limit", "offset"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("flag %q not registered on workflow audit-list", name)
		}
	}
	if got, _ := cmd.Flags().GetInt("limit"); got != 20 {
		t.Errorf("limit default = %d, want 20", got)
	}
}

func TestWorkflowAutoReviewFlags(t *testing.T) {
	cmd := workflowAutoReviewCmd
	for _, name := range []string{"instance-name", "db", "sql", "ids", "execute", "remark"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("flag %q not registered on workflow auto-review", name)
		}
	}
	if got, _ := cmd.Flags().GetBool("execute"); got != false {
		t.Errorf("execute default = %v, want false", got)
	}
}
