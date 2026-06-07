package cmd

import "testing"

func TestInstanceListFlags(t *testing.T) {
	cmd := instanceListCmd

	type flagTest struct {
		name      string
		flagName  string
		wantType  string
		checkFunc func(t *testing.T)
	}

	tests := []flagTest{
		{
			name:     "type",
			flagName: "type",
			wantType: "string",
			checkFunc: func(t *testing.T) {
				got, _ := cmd.Flags().GetString("type")
				if got != "" {
					t.Errorf("default = %q, want ''", got)
				}
			},
		},
		{
			name:     "db-type",
			flagName: "db-type",
			wantType: "string",
			checkFunc: func(t *testing.T) {
				got, _ := cmd.Flags().GetString("db-type")
				if got != "" {
					t.Errorf("default = %q, want ''", got)
				}
			},
		},
		{
			name:     "search",
			flagName: "search",
			wantType: "string",
			checkFunc: func(t *testing.T) {
				got, _ := cmd.Flags().GetString("search")
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
