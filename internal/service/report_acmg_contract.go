package service

import (
	"encoding/json"
	"errors"
	"github.com/SchemaBio/Octopus/internal/model"
)

func validateReportACMGContract(raw, contract string) error {
	if contract != model.ReportContractV2 {
		return errors.New("only report-snapshot-v2 is supported")
	}
	if !json.Valid([]byte(raw)) {
		return errors.New("valid report snapshot is required")
	}
	return nil
}
