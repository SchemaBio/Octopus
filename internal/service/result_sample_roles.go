package service

import (
	"fmt"
	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/SchemaBio/Octopus/internal/resultengine"
)

func attachSampleColumnRoles(dataset *model.ResultDataset, manifest map[string]interface{}) error {
	records, ok := manifest["sample_column_roles"].(map[string]interface{})
	if !ok {
		return nil
	}
	raw, exists := records[dataset.Table]
	if !exists {
		return nil
	}
	record, ok := raw.(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid sample role record")
	}
	if record["source_sha256"] != dataset.ObjectSHA256 {
		return fmt.Errorf("sample role source checksum mismatch")
	}
	rawRoles, ok := record["roles"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid sample roles")
	}
	roles := map[string]string{}
	for k, v := range rawRoles {
		value, ok := v.(string)
		if !ok {
			return fmt.Errorf("invalid sample role value")
		}
		roles[k] = value
	}
	if err := resultengine.ValidateSampleColumnRoles(roles); err != nil {
		return err
	}
	dataset.SampleColumnRoles = roles
	return nil
}
