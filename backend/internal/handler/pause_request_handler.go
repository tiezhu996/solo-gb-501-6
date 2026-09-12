package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"sterile-packaging-release-control/backend/internal/dto"
	"sterile-packaging-release-control/backend/internal/repository"
	"sterile-packaging-release-control/backend/internal/service"
	"sterile-packaging-release-control/backend/internal/util"
)

type PauseRequestHandler struct{ service service.PauseRequestService }

func NewPauseRequestHandler(pauseRequestService service.PauseRequestService) *PauseRequestHandler {
	return &PauseRequestHandler{service: pauseRequestService}
}

func (h *PauseRequestHandler) List(c *gin.Context) {
	var filter repository.PauseRequestFilter
	if err := c.ShouldBindQuery(&filter); err != nil {
		util.RespondError(c, util.BadRequest(err.Error()))
		return
	}
	result, err := h.service.List(c.Request.Context(), filter)
	if err != nil {
		util.RespondError(c, err)
		return
	}
	util.Respond(c, http.StatusOK, result)
}

func (h *PauseRequestHandler) Get(c *gin.Context) {
	id, ok := util.ParseID(c)
	if !ok {
		return
	}
	request, err := h.service.Get(c.Request.Context(), id)
	if err != nil {
		util.RespondError(c, err)
		return
	}
	util.Respond(c, http.StatusOK, request)
}

func (h *PauseRequestHandler) Create(c *gin.Context) {
	batchID, ok := util.ParseID(c)
	if !ok {
		return
	}
	var input dto.CreatePauseRequestRequest
	if !util.BindJSON(c, &input) {
		return
	}
	request, err := h.service.Create(c.Request.Context(), ActorFromContext(c), batchID, input.Reason)
	if err != nil {
		util.RespondError(c, err)
		return
	}
	util.Respond(c, http.StatusCreated, request)
}

func (h *PauseRequestHandler) Review(c *gin.Context) {
	id, ok := util.ParseID(c)
	if !ok {
		return
	}
	var input dto.ReviewPauseRequestRequest
	if !util.BindJSON(c, &input) {
		return
	}
	request, err := h.service.Review(c.Request.Context(), ActorFromContext(c), id, input)
	if err != nil {
		util.RespondError(c, err)
		return
	}
	util.Respond(c, http.StatusOK, request)
}

func (h *PauseRequestHandler) Withdraw(c *gin.Context) {
	id, ok := util.ParseID(c)
	if !ok {
		return
	}
	request, err := h.service.Withdraw(c.Request.Context(), ActorFromContext(c), id)
	if err != nil {
		util.RespondError(c, err)
		return
	}
	util.Respond(c, http.StatusOK, request)
}
