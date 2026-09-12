package model

import (
	"fmt"
	"strings"
	"time"

	"sterile-packaging-release-control/backend/internal/constants"
)

// PauseRequest is an operator's application to pause a running batch. The
// partial unique index guarantees at most one pending request per batch, even
// under concurrent submissions.
type PauseRequest struct {
	Base
	ProductionBatchID uint                         `gorm:"index;not null;uniqueIndex:ux_pause_requests_pending_batch,where:status = 'pending'" json:"productionBatchId"`
	ProductionBatch   ProductionBatch              `json:"productionBatch,omitempty"`
	Reason            string                       `gorm:"size:500;not null" json:"reason"`
	Status            constants.PauseRequestStatus `gorm:"size:20;index;not null;default:'pending'" json:"status"`
	ApplicantID       uint                         `gorm:"index;not null" json:"applicantId"`
	ApplicantName     string                       `gorm:"size:100;not null" json:"applicantName"`
	ReviewerID        *uint                        `gorm:"index" json:"reviewerId"`
	ReviewerName      string                       `gorm:"size:100" json:"reviewerName"`
	ReviewComment     string                       `gorm:"size:500" json:"reviewComment"`
	ReviewedAt        *time.Time                   `json:"reviewedAt"`
}

func (r *PauseRequest) Normalize() {
	r.Reason = strings.TrimSpace(r.Reason)
	r.ApplicantName = strings.TrimSpace(r.ApplicantName)
	r.ReviewerName = strings.TrimSpace(r.ReviewerName)
	r.ReviewComment = strings.TrimSpace(r.ReviewComment)
}

func (r PauseRequest) Validate() error {
	if r.ProductionBatchID == 0 {
		return fmt.Errorf("production batch is required")
	}
	if !r.Status.Valid() {
		return fmt.Errorf("unsupported pause request status: %s", r.Status)
	}
	if len([]rune(r.Reason)) < 3 || len([]rune(r.Reason)) > 500 {
		return fmt.Errorf("pause reason must contain 3-500 characters")
	}
	if r.ApplicantID == 0 || r.ApplicantName == "" {
		return fmt.Errorf("applicant identity is required")
	}
	if len([]rune(r.ApplicantName)) > 100 {
		return fmt.Errorf("applicant name cannot exceed 100 characters")
	}
	if len([]rune(r.ReviewComment)) > 500 {
		return fmt.Errorf("review comment cannot exceed 500 characters")
	}
	if r.Status == constants.PauseRequestPending {
		if r.ReviewedAt != nil || r.ReviewerID != nil {
			return fmt.Errorf("pending request cannot carry review information")
		}
	} else if r.ReviewedAt == nil || r.ReviewerID == nil || r.ReviewerName == "" {
		return fmt.Errorf("reviewed request requires reviewer and review time")
	}
	if r.Status == constants.PauseRequestRejected && r.ReviewComment == "" {
		return fmt.Errorf("rejected request requires a review conclusion")
	}
	return nil
}

func (r PauseRequest) Pending() bool {
	return r.Status == constants.PauseRequestPending
}
