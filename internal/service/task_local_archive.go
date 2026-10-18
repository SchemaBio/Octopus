package service

import "github.com/SchemaBio/Octopus/internal/model"

// localArchiveReady is a completion barrier, not an import attempt. An archive
// directory may exist while the Agent is still copying large result files.
func (s *TaskService) localArchiveReady(task *model.Task, archiveDir string) bool {
	if s.cfg.IsSaaS() || task.Executor == model.ExecutorCVM {
		return true
	}
	if s.sepiida != nil {
		report, err := s.sepiida.GetWorkflowByUUID(task.UUID)
		if err != nil || report == nil || report.Status != model.SepiidaStatusSuccess ||
			!report.Archived || !s.currentWorkflowReport(task, report) {
			return false
		}
		// Once the Agent declares completion, let the importer report genuine
		// missing/corrupt archive errors instead of waiting indefinitely.
		return true
	}
	data, err := (&Archiver{cfg: s.cfg}).readArchiveJSONFile(archiveDir, "outputs.resolved.json")
	if err != nil {
		return false
	}
	return len(data) > 0
}
