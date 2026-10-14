package service

import (
	"encoding/json"
	"github.com/SchemaBio/Octopus/internal/model"
)

func expiredSampleMemberUpdates(member *model.PedigreeMember) map[string]interface{} {
	updates := map[string]interface{}{"sample_id": "", "has_sample": false, "source_sample_fields": "[]"}
	var fields []string
	_ = json.Unmarshal([]byte(member.SourceSampleFields), &fields)
	for _, field := range fields {
		switch field {
		case "name":
			updates[field] = "—"
		case "gender":
			updates[field] = model.GenderUnknown
		case "birth_year":
			updates[field] = nil
		case "phenotypes":
			updates[field] = nil
		}
	}
	return updates
}
