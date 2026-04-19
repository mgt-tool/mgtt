package model_test

import (
	"strings"
	"testing"

	"github.com/mgt-tool/mgtt/internal/model"
	"github.com/mgt-tool/mgtt/internal/providersupport"
)

// TestValidate_DuplicateResourceWarning — two components of the same
// type pointing at the same resource are almost always a copy-paste
// mistake. Emit a warning (not an error — there are legitimate cases
// for two probes against one resource, e.g. different fact sets).
func TestValidate_DuplicateResourceWarning(t *testing.T) {
	m := &model.Model{
		Meta: model.Meta{Name: "dup", Version: "1.0", Providers: []string{"aws"}},
		Components: map[string]*model.Component{
			"rds_primary": {Name: "rds_primary", Type: "rds_instance", Resource: "flowers-stage"},
			"rds_alias":   {Name: "rds_alias", Type: "rds_instance", Resource: "flowers-stage"},
		},
		Order: []string{"rds_primary", "rds_alias"},
	}
	reg := providersupport.NewRegistry()
	reg.Register(&providersupport.Provider{
		Meta:  providersupport.ProviderMeta{Name: "aws"},
		Types: map[string]*providersupport.Type{"rds_instance": {Name: "rds_instance"}},
	})

	result := model.Validate(m, reg)

	if len(result.Warnings) == 0 {
		t.Fatalf("expected at least one warning; got 0")
	}
	var found bool
	for _, w := range result.Warnings {
		if strings.Contains(w.Message, "flowers-stage") && strings.Contains(w.Message, "duplicate") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected duplicate-resource warning naming 'flowers-stage'; got %+v", result.Warnings)
	}
}
