package constants

type PauseRequestStatus string

const (
	PauseRequestPending  PauseRequestStatus = "pending"
	PauseRequestApproved PauseRequestStatus = "approved"
	PauseRequestRejected PauseRequestStatus = "rejected"
)

func (s PauseRequestStatus) Valid() bool {
	switch s {
	case PauseRequestPending, PauseRequestApproved, PauseRequestRejected:
		return true
	default:
		return false
	}
}

// PauseReviewAction is the decision an approver can take on a pending request.
type PauseReviewAction string

const (
	PauseReviewApprove PauseReviewAction = "approve"
	PauseReviewReject  PauseReviewAction = "reject"
)

func (a PauseReviewAction) Valid() bool {
	return a == PauseReviewApprove || a == PauseReviewReject
}
