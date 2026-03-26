package config

import (
	"reflect"
	"testing"
)

// TestConductorConfig_NoStagedField verifies AC7: the Staged bool field must be
// removed from ConductorConfig. This test fails while the field exists and passes
// once it has been deleted as part of the legacy orchestrator removal.
func TestConductorConfig_NoStagedField(t *testing.T) {
	cfgType := reflect.TypeOf(ConductorConfig{})
	for i := 0; i < cfgType.NumField(); i++ {
		if cfgType.Field(i).Name == "Staged" {
			t.Errorf("ConductorConfig still has a 'Staged' field (index %d); "+
				"it must be removed per AC7 (legacy orchestrator removal)", i)
		}
	}
}
