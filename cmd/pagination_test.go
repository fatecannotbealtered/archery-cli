package cmd

import "testing"

func TestOffsetListEnvelope(t *testing.T) {
	// More rows remain: next_offset present and equal to offset+count.
	m := offsetListEnvelope([]int{1, 2}, 10, 2, 50)
	if m["has_more"] != true {
		t.Fatalf("has_more = %v, want true", m["has_more"])
	}
	if m["offset"] != 10 {
		t.Fatalf("offset = %v, want 10", m["offset"])
	}
	if m["next_offset"] != 12 {
		t.Fatalf("next_offset = %v, want 12", m["next_offset"])
	}

	// Last page: has_more false and next_offset omitted entirely.
	last := offsetListEnvelope([]int{1, 2}, 48, 2, 50)
	if last["has_more"] != false {
		t.Fatalf("has_more = %v, want false", last["has_more"])
	}
	if _, ok := last["next_offset"]; ok {
		t.Fatalf("next_offset should be omitted on the last page, got %v", last["next_offset"])
	}
}
