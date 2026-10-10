package service

import (
	"encoding/json"
	"fmt"
)

// The legacy report contract reads archived files and cannot convey selected overlays.
func validateReportACMGContract(raw, contract string) error {
	if contract == "report-snapshot-v2" {
		return nil
	}
	var snapshot reportSnapshot
	if err := json.Unmarshal([]byte(raw), &snapshot); err != nil {
		return err
	}
	for _, row := range snapshot.Reported {
		if stringAdjustment(row.Interpretation, "activeAcmgVersion") == "svcv4" {
			return fmt.Errorf("采用 SVC v4.0 的位点须使用 report-snapshot-v2 报告服务，以保留版本及试行标记")
		}
	}
	return nil
}
