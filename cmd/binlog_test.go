package cmd

import "testing"

func TestBinlogListFlags(t *testing.T) {
	if binlogListCmd.Flags().Lookup("instance") == nil {
		t.Error("flag 'instance' not registered on binlog list")
	}
}

func TestBinlogParseFlags(t *testing.T) {
	cmd := binlogParseCmd
	for _, name := range []string{
		"instance", "start-file", "end-file", "start-pos", "end-pos",
		"start-time", "end-time", "schemas", "tables", "sql-types",
		"num", "rollback", "save-sql", "threads",
	} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("flag %q not registered on binlog parse", name)
		}
	}
	if got, _ := cmd.Flags().GetBool("rollback"); got != false {
		t.Errorf("rollback default = %v, want false", got)
	}
	if got, _ := cmd.Flags().GetInt("num"); got != 0 {
		t.Errorf("num default = %d, want 0", got)
	}
}

func TestBinlogPurgeFlags(t *testing.T) {
	cmd := binlogPurgeCmd
	for _, name := range []string{"instance", "binlog"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("flag %q not registered on binlog purge", name)
		}
	}
}

func TestNonEmptyCSV(t *testing.T) {
	tests := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"a", 1},
		{"a,b,c", 3},
		{" a , b ", 2},
		{"a,,b,", 2},
	}
	for _, tt := range tests {
		if got := nonEmptyCSV(tt.in); len(got) != tt.want {
			t.Errorf("nonEmptyCSV(%q) len = %d, want %d", tt.in, len(got), tt.want)
		}
	}
}
