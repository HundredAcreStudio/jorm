package conductor

import (
	"reflect"
	"testing"
)

// TestConductor_Struct_NoStagedField verifies AC8: the unexported 'staged' field
// must be removed from the Conductor struct. This test fails while the field
// exists and passes once it has been deleted.
func TestConductor_Struct_NoStagedField(t *testing.T) {
	cType := reflect.TypeOf(Conductor{})
	for i := 0; i < cType.NumField(); i++ {
		if cType.Field(i).Name == "staged" {
			t.Errorf("Conductor still has a 'staged' field (index %d); "+
				"it must be removed per AC8 (legacy orchestrator removal)", i)
		}
	}
}

// TestConductorNew_TakesNoStagedParam verifies AC8: conductor.New must accept
// exactly four parameters (model, workDir, env, sink) with no staged bool.
// This test fails to compile while New still requires the staged bool argument,
// and passes once the parameter has been removed.
func TestConductorNew_TakesNoStagedParam(t *testing.T) {
	c := New("haiku", t.TempDir(), nil, nil) // 4 args — fails to compile until staged bool is removed
	if c == nil {
		t.Fatal("New() returned nil")
	}
}

