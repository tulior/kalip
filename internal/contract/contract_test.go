package contract

import (
	"encoding/json"
	"testing"
)

// TestSpliceSchemaHasOneOf verifies that the v3.1 splice
// schema uses oneOf to enforce mutually exclusive edit
// shapes.
func TestSpliceSchemaHasOneOf(t *testing.T) {
	items := editsItems(t)
	if _, ok := items["oneOf"]; !ok {
		t.Fatal("edits.items has no oneOf")
	}
}

// TestSpliceSchemaDisallowsAdditionalProperties verifies
// that edits have additionalProperties: false.
func TestSpliceSchemaDisallowsAdditionalProperties(t *testing.T) {
	items := editsItems(t)
	if v, ok := items["additionalProperties"].(bool); !ok || v {
		t.Fatal("expected additionalProperties: false on edits.items")
	}
}

// TestSpliceSchemaHasThreeShapes verifies that oneOf has
// exactly three variants.
func TestSpliceSchemaHasThreeShapes(t *testing.T) {
	items := editsItems(t)
	oneOf, _ := items["oneOf"].([]any)
	if len(oneOf) != 3 {
		t.Fatalf("expected 3 oneOf variants, got %d", len(oneOf))
	}
}

// editsItems navigates the wrapped OpenAI function
// schema to the edits.items object.
func editsItems(t *testing.T) map[string]any {
	t.Helper()
	var schema map[string]any
	if err := json.Unmarshal(spliceSchemaJSON, &schema); err != nil {
		t.Fatal(err)
	}
	params, _ := schema["parameters"].(map[string]any)
	if params == nil {
		t.Fatal("schema has no parameters")
	}
	props, _ := params["properties"].(map[string]any)
	if props == nil {
		t.Fatal("parameters has no properties")
	}
	edits, _ := props["edits"].(map[string]any)
	if edits == nil {
		t.Fatal("parameters.properties has no edits")
	}
	items, _ := edits["items"].(map[string]any)
	if items == nil {
		t.Fatal("edits has no items")
	}
	return items
}

// TestDescriptionsMatchContract verifies the v3.1 tool
// descriptions are present in the embedded JSON.
func TestDescriptionsMatchContract(t *testing.T) {
	d, err := LoadDescriptions()
	if err != nil {
		t.Fatal(err)
	}
	if d.ReadRef == "" {
		t.Error("missing read_ref description")
	}
	if d.Sh == "" {
		t.Error("missing sh description")
	}
	if d.SpliceBCurrent == "" {
		t.Error("missing splice B_current description")
	}
	if d.SpliceBFixed == "" {
		t.Error("missing splice B_fixed description")
	}
}

// TestReadRefDescriptionDistinguishesRefRoles verifies
// that the read_ref description explicitly distinguishes
// @ref R<n> from Lnn:hh line anchors.
func TestReadRefDescriptionDistinguishesRefRoles(t *testing.T) {
	d, err := LoadDescriptions()
	if err != nil {
		t.Fatal(err)
	}
	// The description must mention both the @ref R<n>
	// identifier and the Lnn:hh anchor format.
	for _, want := range []string{"@ref R", "splice.ref", "L38"} {
		if !contains(d.ReadRef, want) {
			t.Errorf("read_ref description missing %q: %q", want, d.ReadRef)
		}
	}
}

// TestShDescriptionStatesNoPersistence verifies that the
// sh description explicitly says no state persists across
// calls.
func TestShDescriptionStatesNoPersistence(t *testing.T) {
	d, err := LoadDescriptions()
	if err != nil {
		t.Fatal(err)
	}
	// The description must mention that state does not
	// persist between calls.
	for _, want := range []string{"does not", "persist", "shell"} {
		if !contains(d.Sh, want) {
			t.Errorf("sh description missing %q: %q", want, d.Sh)
		}
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
