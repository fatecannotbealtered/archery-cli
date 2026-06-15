package cmd

import (
	"reflect"
	"testing"

	"github.com/fatecannotbealtered/archery-cli/internal/api"
)

func TestParseResourceGroupObjectType(t *testing.T) {
	cases := []struct {
		in        string
		wantType  api.ResourceGroupObjectType
		wantLabel string
		wantErr   bool
	}{
		{"user", api.ResourceGroupObjectUser, "user", false},
		{"USERS", api.ResourceGroupObjectUser, "user", false},
		{"0", api.ResourceGroupObjectUser, "user", false},
		{"instance", api.ResourceGroupObjectInstance, "instance", false},
		{"1", api.ResourceGroupObjectInstance, "instance", false},
		{"bogus", "", "", true},
	}
	for _, c := range cases {
		gotType, gotLabel, err := parseResourceGroupObjectType(c.in)
		if (err != nil) != c.wantErr {
			t.Errorf("%q: err = %v, wantErr %v", c.in, err, c.wantErr)
			continue
		}
		if err != nil {
			continue
		}
		if gotType != c.wantType || gotLabel != c.wantLabel {
			t.Errorf("%q: got (%v,%q), want (%v,%q)", c.in, gotType, gotLabel, c.wantType, c.wantLabel)
		}
	}
}

func TestParseIntIDs(t *testing.T) {
	got, err := parseIntIDs("3, 4 ,10")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(got, []int{3, 4, 10}) {
		t.Errorf("got %v, want [3 4 10]", got)
	}

	for _, bad := range []string{"", "0", "-1", "a", "3,x"} {
		if _, err := parseIntIDs(bad); err == nil {
			t.Errorf("parseIntIDs(%q) = nil error, want error", bad)
		}
	}
}

func TestFilterUsers(t *testing.T) {
	rows := []api.UserRow{
		{ID: 1, Username: "alice", Display: "Alice", Email: "a@x.io"},
		{ID: 2, Username: "bob", Display: "Bob", Email: "b@y.io"},
		{ID: 3, Username: "carol", Display: "Carol", Email: "c@x.io"},
	}
	if got := filterUsers(rows, ""); len(got) != 3 {
		t.Errorf("empty search returned %d rows, want 3", len(got))
	}
	// Match on email domain substring.
	if got := filterUsers(rows, "x.io"); len(got) != 2 {
		t.Errorf("search x.io returned %d rows, want 2", len(got))
	}
	// Case-insensitive username match.
	if got := filterUsers(rows, "BOB"); len(got) != 1 || got[0].ID != 2 {
		t.Errorf("search BOB = %+v, want bob", got)
	}
}

func TestPageUsers(t *testing.T) {
	rows := make([]api.UserRow, 5)
	for i := range rows {
		rows[i] = api.UserRow{ID: i + 1}
	}
	if got := pageUsers(rows, 0, 2); len(got) != 2 || got[0].ID != 1 {
		t.Errorf("offset 0 limit 2 = %+v", got)
	}
	if got := pageUsers(rows, 4, 10); len(got) != 1 || got[0].ID != 5 {
		t.Errorf("offset 4 limit 10 = %+v", got)
	}
	if got := pageUsers(rows, 10, 2); got != nil {
		t.Errorf("offset past end = %+v, want nil", got)
	}
}
