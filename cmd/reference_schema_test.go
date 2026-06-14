package cmd

import "testing"

// TestReference_EveryLeafCommandHasRealSchemaAndExample guards against
// output_schema regressing to a stub: every leaf command in the reference tree
// must carry a schema label that exists in the schemas map with a non-empty
// field list, plus at least one runnable example. This keeps `reference` a
// usable source of truth for agents.
func TestReference_EveryLeafCommandHasRealSchemaAndExample(t *testing.T) {
	tree := buildReferenceTree(rootCmd)
	if len(tree.Commands) == 0 {
		t.Fatal("reference enumerated zero commands")
	}
	if len(tree.Schemas) == 0 {
		t.Fatal("reference enumerated zero schemas")
	}

	leaves := 0
	walkRefCommands(tree.Commands, func(c refCommand) {
		if len(c.Commands) > 0 {
			return
		}
		leaves++
		if c.OutputSchema == "" {
			t.Errorf("%s: empty output_schema", c.Path)
			return
		}
		schema, ok := tree.Schemas[c.OutputSchema]
		if !ok {
			t.Errorf("%s: output_schema %q not defined in schemas map", c.Path, c.OutputSchema)
			return
		}
		if len(schema.Fields) == 0 {
			t.Errorf("%s: schema %q has no fields (stub)", c.Path, c.OutputSchema)
		}
		if len(c.Examples) == 0 {
			t.Errorf("%s: no examples", c.Path)
		}
	})
	if leaves == 0 {
		t.Fatal("reference walked zero leaf commands")
	}
}

// TestReference_NoOrphanSchemas ensures every schema label defined in the
// schemas map is actually referenced by a leaf command, so the map cannot drift
// into carrying dead entries.
func TestReference_NoOrphanSchemas(t *testing.T) {
	tree := buildReferenceTree(rootCmd)
	used := map[string]bool{}
	walkRefCommands(tree.Commands, func(c refCommand) {
		if len(c.Commands) == 0 && c.OutputSchema != "" {
			used[c.OutputSchema] = true
		}
	})
	for label := range tree.Schemas {
		if !used[label] {
			t.Errorf("schema %q is defined but referenced by no leaf command", label)
		}
	}
}
