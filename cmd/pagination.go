package cmd

// offsetListEnvelope builds the JSON shape shared by the offset-paginated list
// commands (archive, slowquery, query). It adds an explicit `offset` echo and a
// `next_offset` cursor (present only when more rows remain) so an agent can page
// deterministically without re-deriving the arithmetic from offset+count<total.
func offsetListEnvelope(items any, offset, count, total int) map[string]any {
	hasMore := offset+count < total
	m := map[string]any{
		"items":    items,
		"total":    total,
		"count":    count,
		"offset":   offset,
		"has_more": hasMore,
	}
	if hasMore {
		m["next_offset"] = offset + count
	}
	return m
}
