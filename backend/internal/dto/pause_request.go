package dto

import "sterile-packaging-release-control/backend/internal/constants"

type CreatePauseRequestRequest struct {
	Reason string `json:"reason" binding:"required,min=3,max=500"`
}

type ReviewPauseRequestRequest struct {
	Action  constants.PauseReviewAction `json:"action" binding:"required"`
	Comment string                      `json:"comment" binding:"max=500"`
}
