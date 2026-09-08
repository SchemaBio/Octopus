package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (s *TaskService) enqueueCVM(ctx context.Context, id string, actor model.OverlayActor) (*model.Task, error) {
	var task model.Task
	err := database.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("uuid = ?", id).First(&task).Error; err != nil {
			return err
		}
		if task.ExecutionPhase != "" && task.ExecutionPhase != "idle" && task.ExecutionPhase != "terminal" {
			// A pre-outbox deployment or an interrupted migration may leave an
			// active dispatching task without its durable submission row. Repeated
			// start requests must repair that intent while retaining the same
			// attempt; running/archiving/terminating phases do not need a new
			// submission.
			if cvmPhaseNeedsSubmission(task.ExecutionPhase) && task.ExecutionAttemptID != "" {
				now := time.Now().UTC()
				if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&model.CVMSubmission{
					AttemptID: task.ExecutionAttemptID, TaskUUID: task.UUID, NextRetryAt: now,
				}).Error; err != nil {
					return err
				}
			}
			return nil
		}
		if task.Status != model.TaskStatusQueued && task.Status != model.TaskStatusFailed && task.Status != model.TaskStatusCancelled && task.Status != model.TaskStatusWaitingData {
			return fmt.Errorf("task cannot be started from %s", task.Status)
		}
		if task.Status == model.TaskStatusWaitingData {
			if ready, reason := s.checkDataReady(&task); !ready {
				return fmt.Errorf("data not ready: %s", reason)
			}
		}
		// Legacy live attempts are adopted without changing their identity.
		if task.ExecutionAttemptID == "" || cvmAttemptStateTerminal(task.VMStatus) || task.ExecutionPhase == "terminal" {
			task.ExecutionAttemptID = uuid.NewString()
			task.LastCVMEventVersion = 0
			task.CVMInstanceID = ""
			task.VMStatus = ""
			task.CVMDispatchNextRetryAt = nil
			task.CVMDispatchRetryDeadlineAt = nil
			task.CVMDispatchRetryCount = 0
			task.StartedAt = nil
			task.FinishedAt = nil
		}
		task.Status = model.TaskStatusQueued
		task.ExecutionPhase = "dispatching"
		task.VMStatus = "DISPATCHING"
		task.Error = ""
		task.ExecutionReasonCode = ""
		task.CVMArchiveStagedAt = nil
		task.CVMArchiveTerminationNotifiedAt = nil
		task.Version++
		now := time.Now().UTC()
		task.PhaseUpdatedAt = &now
		if err := tx.Save(&task).Error; err != nil {
			return err
		}
		return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&model.CVMSubmission{AttemptID: task.ExecutionAttemptID, TaskUUID: task.UUID, NextRetryAt: now}).Error
	})
	return &task, err
}

func cvmPhaseNeedsSubmission(phase string) bool {
	switch strings.ToLower(strings.TrimSpace(phase)) {
	case "dispatching", "waiting_quota", "waiting_capacity":
		return true
	default:
		return false
	}
}

func cvmTerminationReason(task *model.Task, fallback string) string {
	if task != nil {
		switch task.Status {
		case model.TaskStatusCompleted:
			return model.OverlayTaskEventCompleted
		case model.TaskStatusFailed:
			return model.OverlayTaskEventFailed
		case model.TaskStatusCancelled:
			return model.OverlayTaskEventCancelled
		}
	}
	if strings.TrimSpace(fallback) == "" {
		return model.OverlayTaskEventCancelled
	}
	return fallback
}

func (s *TaskService) cancelCVM(ctx context.Context, id string, requests ...model.CVMCancelRequest) (*model.Task, error) {
	var task model.Task
	err := database.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("uuid = ?", id).First(&task).Error; err != nil {
			return err
		}
		request := model.CVMCancelRequest{TaskUUID: task.UUID, AttemptID: task.ExecutionAttemptID, Reason: model.OverlayTaskEventCancelled}
		if len(requests) > 0 {
			request = requests[0]
			if request.TaskUUID == "" {
				request.TaskUUID = task.UUID
			}
			if request.AttemptID == "" {
				request.AttemptID = task.ExecutionAttemptID
			}
			if request.Reason == "" {
				request.Reason = model.OverlayTaskEventCancelled
			}
		}
		if request.TaskUUID != task.UUID {
			return fmt.Errorf("cancellation task_uuid does not match task")
		}
		if request.AttemptID != task.ExecutionAttemptID {
			return fmt.Errorf("cancellation attempt_id does not match current execution")
		}
		if task.ExecutionPhase == "terminal" {
			return nil
		}
		if task.ExecutionPhase == "terminating" {
			// Cancellation is a durable command. A repeated stop/delete request
			// must repair a missing outbox row after an upgrade, but it must not
			// bump the task version or emit another lifecycle event while the
			// original release is already in flight.
			if task.ExecutionAttemptID == "" {
				return nil
			}
			payload, err := json.Marshal(request)
			if err != nil {
				return err
			}
			now := time.Now().UTC()
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&model.CVMCancellation{
				AttemptID: task.ExecutionAttemptID, TaskUUID: task.UUID, Payload: string(payload), NextRetryAt: now,
			}).Error; err != nil {
				return err
			}
			return tx.Model(&model.CVMSubmission{}).Where("attempt_id = ? AND delivered = FALSE", task.ExecutionAttemptID).Updates(map[string]interface{}{
				"delivered":  true,
				"last_error": "dispatch cancelled before acknowledgement",
			}).Error
		}
		if task.ExecutionAttemptID == "" {
			now := time.Now().UTC()
			task.Status = model.TaskStatusCancelled
			task.ExecutionPhase = "terminal"
			task.FinishedAt = &now
			task.Version++
			return tx.Save(&task).Error
		}
		task.ExecutionPhase = "terminating"
		task.VMStatus = "TERMINATING"
		task.Version++
		now := time.Now().UTC()
		task.PhaseUpdatedAt = &now
		// Keep the task visible until Squid confirms that its resource is gone.
		if err := tx.Save(&task).Error; err != nil {
			return err
		}
		payload, err := json.Marshal(request)
		if err != nil {
			return err
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&model.CVMCancellation{
			AttemptID: task.ExecutionAttemptID, TaskUUID: task.UUID, Payload: string(payload), NextRetryAt: now,
		}).Error; err != nil {
			return err
		}
		// A pending dispatch must never be sent after cancellation. If a worker
		// already sent it, the cancellation outbox still reconciles that same
		// attempt with Squid.
		return tx.Model(&model.CVMSubmission{}).Where("attempt_id = ? AND delivered = FALSE", task.ExecutionAttemptID).Updates(map[string]interface{}{
			"delivered":  true,
			"last_error": "dispatch cancelled before acknowledgement",
		}).Error
	})
	return &task, err
}

func (s *TaskService) StartExecutionDelivery(ctx context.Context) {
	if s.overlay == nil {
		return
	}
	s.adoptLegacyExecutions(ctx)
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			s.deliverExecutionEvents(ctx)
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			s.deliverExecutions(ctx)
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			s.deliverCVMCancellations(ctx)
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (s *TaskService) deliverExecutions(ctx context.Context) {
	db := database.GetDB().WithContext(ctx)
	var jobs []model.CVMSubmission
	if err := db.Where("delivered = FALSE AND next_retry_at <= ?", time.Now().UTC()).Order("created_at").Limit(100).Find(&jobs).Error; err != nil {
		return
	}
	for _, job := range jobs {
		now := time.Now().UTC()
		claim := db.Model(&model.CVMSubmission{}).Where("attempt_id = ? AND delivered = FALSE AND next_retry_at <= ?", job.AttemptID, now).Update("next_retry_at", now.Add(2*time.Minute))
		if claim.Error != nil || claim.RowsAffected != 1 {
			continue
		}
		task, err := s.repo.FindByUUID(job.TaskUUID)
		if err != nil {
			// A task removed during an upgrade must not be dispatched from a
			// stale submission. There is no safe execution request to rebuild.
			_ = db.Model(&job).Update("delivered", true).Error
			continue
		}
		if task.ExecutionAttemptID != job.AttemptID {
			_ = db.Model(&job).Update("delivered", true).Error
			continue
		}
		if cvmAttemptNoLongerDispatchable(task) {
			// A cancellation or a terminal callback owns this attempt now. The
			// durable cancellation/event outbox will perform the corresponding
			// Squid action; never submit the original request after that fence.
			_ = db.Model(&job).Updates(map[string]interface{}{
				"delivered":  true,
				"last_error": "dispatch skipped because the attempt is terminating or terminal",
			}).Error
			continue
		}
		request, err := s.loadOrPersistCVMSubmissionRequest(ctx, db, &job, task)
		var response *model.CVMDispatchResponse
		if err == nil {
			response, err = s.overlay.DispatchCVMTask(ctx, request)
		}
		if err != nil {
			_ = db.Model(&job).Updates(map[string]interface{}{"last_error": err.Error(), "next_retry_at": time.Now().UTC().Add(15 * time.Second)}).Error
			if !OverlayDispatchOutcomeUnknown(err) {
				// A cancellation may win while the HTTP request is in flight. Do
				// not turn that newer terminal intent into a dispatch failure when
				// Squid rejects the now-tombstoned attempt.
				latest, findErr := s.repo.FindByUUID(job.TaskUUID)
				if findErr == nil && latest != nil && latest.ExecutionAttemptID == job.AttemptID && !cvmAttemptNoLongerDispatchable(latest) {
					// The HTTP rejection proves only that this delivery attempt was
					// not acknowledged. Squid may already have a durable terminal
					// record for the same attempt, so route the failure through the
					// normal terminal event and let Squid reconcile/refund it
					// idempotently instead of bypassing cleanup.
					latest.ExecutionReasonCode = "DISPATCH_FAILED"
					latest.Status = model.TaskStatusFailed
					latest.ExecutionPhase = "terminal"
					latest.VMStatus = "LAUNCH_FAILED"
					latest.Error = err.Error()
					now := time.Now().UTC()
					latest.FinishedAt = &now
					latest.PhaseUpdatedAt = &now
					if s.repo.Update(latest) == nil {
						_ = db.Model(&job).Update("delivered", true).Error
					}
				} else if findErr == nil && latest != nil && latest.ExecutionAttemptID == job.AttemptID {
					_ = db.Model(&job).Update("delivered", true).Error
				}
			}
			continue
		}
		// Squid's versioned outbox is authoritative; this response only acknowledges
		// durable receipt, and cannot roll back a callback which arrived first.
		if response != nil && response.Accepted {
			_ = db.Model(&job).Update("delivered", true).Error
		}
	}
}

func cvmAttemptNoLongerDispatchable(task *model.Task) bool {
	if task == nil {
		return true
	}
	phase := strings.ToLower(strings.TrimSpace(task.ExecutionPhase))
	if phase == "terminating" || phase == "terminal" {
		return true
	}
	switch task.Status {
	case model.TaskStatusCompleted, model.TaskStatusFailed, model.TaskStatusCancelled:
		return true
	default:
		return false
	}
}

// loadOrPersistCVMSubmissionRequest freezes the first server-generated
// dispatch payload for an attempt. The conditional update is the fence for
// two Octopus workers which both observed an empty snapshot after a restart;
// a worker which loses the race reloads the winner's exact payload instead of
// sending a request built from different presigned URLs or task inputs.
func (s *TaskService) loadOrPersistCVMSubmissionRequest(ctx context.Context, db *gorm.DB, job *model.CVMSubmission, task *model.Task) (model.CVMDispatchRequest, error) {
	var request model.CVMDispatchRequest
	if strings.TrimSpace(job.RequestJSON) != "" {
		if err := json.Unmarshal([]byte(job.RequestJSON), &request); err != nil {
			return request, fmt.Errorf("invalid persisted CVM dispatch request: %w", err)
		}
		return request, nil
	}

	built, err := s.buildCVMDispatchRequest(ctx, model.OverlayActor{UserID: task.CreatedBy, OrgID: task.ExternalOrgID}, task)
	if err != nil {
		return request, err
	}
	payload, err := json.Marshal(built)
	if err != nil {
		return request, err
	}
	result := db.Model(&model.CVMSubmission{}).
		Where("attempt_id = ? AND COALESCE(request_json, '') = ?", job.AttemptID, "").
		Update("request_json", string(payload))
	if result.Error != nil {
		return request, result.Error
	}
	if result.RowsAffected == 1 {
		job.RequestJSON = string(payload)
		return built, nil
	}

	var stored model.CVMSubmission
	if err := db.Where("attempt_id = ?", job.AttemptID).First(&stored).Error; err != nil {
		return request, err
	}
	if strings.TrimSpace(stored.RequestJSON) == "" {
		return request, fmt.Errorf("CVM dispatch request snapshot was not persisted")
	}
	if err := json.Unmarshal([]byte(stored.RequestJSON), &request); err != nil {
		return request, fmt.Errorf("invalid persisted CVM dispatch request: %w", err)
	}
	job.RequestJSON = stored.RequestJSON
	return request, nil
}

func (s *TaskService) deliverExecutionEvents(ctx context.Context) {
	db := database.GetDB().WithContext(ctx)
	var events []model.ExecutionOutbox
	if db.Where("delivered = FALSE AND next_retry_at <= ?", time.Now().UTC()).Order("created_at").Limit(100).Find(&events).Error != nil {
		return
	}
	for _, event := range events {
		var request model.OverlayTaskEventRequest
		if err := json.Unmarshal([]byte(event.Payload), &request); err != nil {
			// A corrupt outbox row cannot be repaired by retrying the HTTP call,
			// but it must remain observable and out of the hot loop. Keep it in
			// the outbox with a long retry delay so an operator can repair the
			// payload or inspect the error without silently dropping the event.
			_ = db.Model(&model.ExecutionOutbox{}).Where("id = ?", event.ID).Updates(map[string]interface{}{
				"last_error":    fmt.Sprintf("invalid execution event payload: %v", err),
				"next_retry_at": time.Now().UTC().Add(24 * time.Hour),
			}).Error
			continue
		}
		err := s.overlay.EmitTaskEvent(ctx, request)
		if err == nil {
			_ = db.Model(&event).Update("delivered", true).Error
		} else {
			_ = db.Model(&event).Updates(map[string]interface{}{"last_error": err.Error(), "next_retry_at": time.Now().UTC().Add(15 * time.Second)}).Error
		}
	}
}

func (s *TaskService) deliverCVMCancellations(ctx context.Context) {
	db := database.GetDB().WithContext(ctx)
	var jobs []model.CVMCancellation
	if db.Where("delivered = FALSE AND next_retry_at <= ?", time.Now().UTC()).Order("created_at").Limit(100).Find(&jobs).Error != nil {
		return
	}
	for _, job := range jobs {
		now := time.Now().UTC()
		claim := db.Model(&model.CVMCancellation{}).Where("attempt_id = ? AND delivered = FALSE AND next_retry_at <= ?", job.AttemptID, now).Update("next_retry_at", now.Add(2*time.Minute))
		if claim.Error != nil || claim.RowsAffected != 1 {
			continue
		}
		var request model.CVMCancelRequest
		if err := json.Unmarshal([]byte(job.Payload), &request); err != nil {
			_ = db.Model(&model.CVMCancellation{}).Where("attempt_id = ?", job.AttemptID).Updates(map[string]interface{}{
				"last_error":    fmt.Sprintf("invalid CVM cancellation payload: %v", err),
				"next_retry_at": time.Now().UTC().Add(24 * time.Hour),
			}).Error
			continue
		}
		if err := s.overlay.CancelCVMTask(ctx, request); err != nil {
			_ = db.Model(&model.CVMCancellation{}).Where("attempt_id = ?", job.AttemptID).Updates(map[string]interface{}{
				"last_error":    err.Error(),
				"next_retry_at": time.Now().UTC().Add(15 * time.Second),
			}).Error
			continue
		}
		_ = db.Model(&model.CVMCancellation{}).Where("attempt_id = ?", job.AttemptID).Updates(map[string]interface{}{
			"delivered": true, "last_error": "",
		}).Error
	}
}

// Upgrade recovery only adopts the stored identity; it never fabricates a new
// attempt for an execution which might still own a cloud instance.
func (s *TaskService) adoptLegacyExecutions(ctx context.Context) {
	db := database.GetDB().WithContext(ctx)
	var tasks []model.Task
	// Rows created before the durable dispatcher did not have an outbox. The
	// phase migration marks those rows as bootstrapping/running, so recovery
	// must inspect those phases too. Only rows with no observed Squid callback
	// are selected; once a callback advances LastCVMEventVersion, normal
	// lifecycle handling owns the record.
	recoveryPhases := []string{
		"dispatching", "waiting_quota", "waiting_capacity",
		"bootstrapping", "running", "archiving", "terminating", "terminal",
	}
	if db.Where("executor = ? AND execution_attempt_id <> '' AND (COALESCE(execution_phase, '') = '' OR (execution_phase IN ? AND last_cvm_event_version = 0))", model.ExecutorCVM, recoveryPhases).Find(&tasks).Error != nil {
		return
	}
	for _, candidate := range tasks {
		_ = db.Transaction(func(tx *gorm.DB) error {
			var task model.Task
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", candidate.ID).First(&task).Error; err != nil {
				return err
			}
			phase := strings.ToLower(strings.TrimSpace(task.ExecutionPhase))
			legacyTerminal := cvmTerminalTaskStatus(task.Status)
			dispatchPhase := cvmPhaseNeedsSubmission(phase) || cvmDispatchVMStatus(task.VMStatus)
			needsSave := false

			// A terminal task must fence the old attempt before a new start can
			// reuse the task. Keep the exact attempt ID and ask Squid to reconcile
			// it; never create a replacement cloud request during migration.
			if legacyTerminal {
				if phase != "terminating" {
					task.ExecutionPhase = "terminating"
					task.VMStatus = "TERMINATING"
					needsSave = true
				}
			} else if phase == "" {
				switch {
				case dispatchPhase:
					task.ExecutionPhase = "dispatching"
				case task.Status == model.TaskStatusRunning:
					task.ExecutionPhase = "running"
				default:
					task.ExecutionPhase = "bootstrapping"
				}
				needsSave = true
			} else if dispatchPhase && phase != "dispatching" {
				// Waiting phases were used by an early dispatcher. Normalize them
				// to the durable dispatch phase so the repaired submission is sent
				// exactly once.
				task.ExecutionPhase = "dispatching"
				needsSave = true
			}

			if needsSave {
				now := time.Now().UTC()
				task.PhaseUpdatedAt = &now
				task.Version++
				if err := tx.Save(&task).Error; err != nil {
					return err
				}
			} else if err := ensureLegacyExecutionOutbox(tx, &task); err != nil {
				return err
			}
			if task.ExecutionPhase == "dispatching" {
				now := time.Now().UTC()
				return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&model.CVMSubmission{TaskUUID: task.UUID, AttemptID: task.ExecutionAttemptID, NextRetryAt: now}).Error
			}
			return nil
		})
	}
}

func cvmTerminalTaskStatus(status model.TaskStatus) bool {
	switch status {
	case model.TaskStatusCompleted, model.TaskStatusPendingInterpretation, model.TaskStatusFailed, model.TaskStatusCancelled:
		return true
	default:
		return false
	}
}

func cvmDispatchVMStatus(status string) bool {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "DISPATCHING", "WAITING_QUOTA", "WAITING_CAPACITY":
		return true
	default:
		return false
	}
}

// ensureLegacyExecutionOutbox recreates a missing event without saving the
// task. This matters for bootstrapping/running legacy rows: incrementing their
// version on every restart would manufacture a new event each time and could
// make an old worker appear newer than a cancellation.
func ensureLegacyExecutionOutbox(tx *gorm.DB, task *model.Task) error {
	for _, kind := range model.ExecutionOutboxEvents(task) {
		outbox, err := model.NewExecutionOutbox(task, kind)
		if err != nil {
			return err
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(outbox).Error; err != nil {
			return err
		}
	}
	return nil
}
