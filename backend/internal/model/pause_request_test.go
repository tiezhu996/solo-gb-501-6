package model

import (
	"testing"
	"time"

	"sterile-packaging-release-control/backend/internal/constants"
)

func validPauseRequest() PauseRequest {
	return PauseRequest{
		ProductionBatchID: 1, Reason: "设备点检需要停机", Status: constants.PauseRequestPending,
		ApplicantID: 2, ApplicantName: "产线操作员",
	}
}

func TestPauseRequestValidation(t *testing.T) {
	request := validPauseRequest()
	if err := request.Validate(); err != nil {
		t.Fatalf("valid pending request rejected: %v", err)
	}
	if !request.Pending() {
		t.Fatal("new request must be pending")
	}

	request.Reason = "停"
	if err := request.Validate(); err == nil {
		t.Fatal("short reason must fail")
	}

	request = validPauseRequest()
	now := time.Now()
	reviewerID := uint(3)
	request.Status = constants.PauseRequestRejected
	request.ReviewerID = &reviewerID
	request.ReviewerName = "放行审批员"
	request.ReviewedAt = &now
	if err := request.Validate(); err == nil {
		t.Fatal("rejected request without conclusion must fail")
	}
	request.ReviewComment = "生产计划紧张，驳回暂停"
	if err := request.Validate(); err != nil {
		t.Fatalf("rejected request with conclusion should pass: %v", err)
	}

	request = validPauseRequest()
	request.Status = constants.PauseRequestApproved
	if err := request.Validate(); err == nil {
		t.Fatal("approved request without reviewer must fail")
	}
}

func TestPauseReviewAction(t *testing.T) {
	if !constants.PauseReviewApprove.Valid() || !constants.PauseReviewReject.Valid() {
		t.Fatal("approve and reject must be valid review actions")
	}
	if constants.PauseReviewAction("hold").Valid() {
		t.Fatal("unknown review action must be invalid")
	}
	if !constants.PauseRequestPending.Valid() || !constants.PauseRequestApproved.Valid() || !constants.PauseRequestRejected.Valid() {
		t.Fatal("all pause request statuses must be valid")
	}
	if constants.PauseRequestStatus("done").Valid() {
		t.Fatal("unknown pause request status must be invalid")
	}
}
