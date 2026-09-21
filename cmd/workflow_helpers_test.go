package cmd

import (
	"strings"
	"testing"
)

// TestWorkflowWebURL pins the browser link to Archery's actual detail route.
// "sqlworkflow/" is the workflow *list* page and takes no id segment, so the
// link the CLI used to emit — host + /sqlworkflow/<id>/ — matched no route and
// 404'd for every workflow it ever printed, on v1.8.5 through v1.9.x alike.
func TestWorkflowWebURL(t *testing.T) {
	cases := []struct{ host, want string }{
		{"https://archery.example.com", "https://archery.example.com/detail/46896/"},
		{"https://archery.example.com/", "https://archery.example.com/detail/46896/"},
	}
	for _, c := range cases {
		if got := workflowWebURL(c.host, 46896); got != c.want {
			t.Fatalf("workflowWebURL(%q) = %q, want %q", c.host, got, c.want)
		}
	}
	if got := workflowWebURL("https://archery.example.com", 1); strings.Contains(got, "/sqlworkflow/") {
		t.Fatalf("still pointing at the list route: %q", got)
	}
}
