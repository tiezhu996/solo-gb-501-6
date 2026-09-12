package service

import (
	"context"
	"strings"
	"time"

	"sterile-packaging-release-control/backend/internal/constants"
	"sterile-packaging-release-control/backend/internal/dto"
	"sterile-packaging-release-control/backend/internal/model"
	"sterile-packaging-release-control/backend/internal/repository"
	"sterile-packaging-release-control/backend/internal/util"
)

type PauseRequestService interface {
	List(context.Context, repository.PauseRequestFilter) (dto.PageResult[model.PauseRequest], error)
	Get(context.Context, uint) (*model.PauseRequest, error)
	Create(context.Context, Actor, uint, string) (*model.PauseRequest, error)
	Review(context.Context, Actor, uint, dto.ReviewPauseRequestRequest) (*model.PauseRequest, error)
}

type pauseRequestService struct {
	repo      repository.PauseRequestRepository
	batchRepo repository.BatchRepository
	audit     AuditService
	tx        repository.Transactor
}

func NewPauseRequestService(repo repository.PauseRequestRepository, batchRepo repository.BatchRepository, audit AuditService, tx repository.Transactor) PauseRequestService {
	return &pauseRequestService{repo: repo, batchRepo: batchRepo, audit: audit, tx: tx}
}

func (s *pauseRequestService) List(ctx context.Context, filter repository.PauseRequestFilter) (dto.PageResult[model.PauseRequest], error) {
	query := filter.PageQuery.Normalize()
	filter.PageQuery = query
	items, total, err := s.repo.List(ctx, filter)
	return dto.PageResult[model.PauseRequest]{Items: items, Total: total, Page: query.Page, PageSize: query.PageSize}, err
}

func (s *pauseRequestService) Get(ctx context.Context, id uint) (*model.PauseRequest, error) {
	return s.repo.Find(ctx, id)
}

func (s *pauseRequestService) Create(ctx context.Context, actor Actor, batchID uint, reason string) (*model.PauseRequest, error) {
	reason = strings.TrimSpace(reason)
	if len([]rune(reason)) < 3 {
		return nil, util.BadRequest("暂停原因至少填写 3 个字符")
	}
	var request *model.PauseRequest
	err := s.tx.WithinTransaction(ctx, func(txCtx context.Context) error {
		batch, err := s.batchRepo.FindForUpdate(txCtx, batchID)
		if err != nil {
			return err
		}
		if batch.Status != constants.BatchStatusRunning {
			return util.Conflict("只有生产中的批次才能申请暂停")
		}
		pending, err := s.repo.CountPendingForBatch(txCtx, batch.ID)
		if err != nil {
			return err
		}
		if pending > 0 {
			return util.Conflict("该批次已有待处理的暂停申请")
		}
		request = &model.PauseRequest{
			ProductionBatchID: batch.ID, Reason: reason, Status: constants.PauseRequestPending,
			ApplicantID: actor.ID, ApplicantName: actor.Name,
		}
		request.Normalize()
		if err := request.Validate(); err != nil {
			return util.BadRequest(err.Error())
		}
		if err := s.repo.Create(txCtx, request); err != nil {
			if util.IsUniqueViolation(err) {
				return util.Conflict("该批次已有待处理的暂停申请")
			}
			return err
		}
		return s.audit.Record(txCtx, actor, "pause_request.created", "PauseRequest", request.ID, nil, request)
	})
	if err != nil {
		return nil, err
	}
	return s.repo.Find(ctx, request.ID)
}

func (s *pauseRequestService) Review(ctx context.Context, actor Actor, id uint, input dto.ReviewPauseRequestRequest) (*model.PauseRequest, error) {
	if !input.Action.Valid() {
		return nil, util.BadRequest("无效的审批动作")
	}
	comment := strings.TrimSpace(input.Comment)
	if input.Action == constants.PauseReviewReject && comment == "" {
		return nil, util.BadRequest("拒绝暂停申请必须填写审批结论")
	}
	var request *model.PauseRequest
	err := s.tx.WithinTransaction(ctx, func(txCtx context.Context) error {
		var err error
		request, err = s.repo.FindForUpdate(txCtx, id)
		if err != nil {
			return err
		}
		if !request.Pending() {
			return util.Conflict("该暂停申请已处理，不能重复审批")
		}
		batch, err := s.batchRepo.FindForUpdate(txCtx, request.ProductionBatchID)
		if err != nil {
			return err
		}
		before := *request
		now := time.Now()
		request.ReviewerID = &actor.ID
		request.ReviewerName = actor.Name
		request.ReviewComment = comment
		request.ReviewedAt = &now
		if input.Action == constants.PauseReviewApprove {
			if batch.Status != constants.BatchStatusRunning {
				return util.Conflict("批次状态已变化，无法执行暂停")
			}
			batchBefore := *batch
			batch.Status = constants.BatchStatusHold
			batch.HoldReason = request.Reason
			batch.Normalize()
			if err := batch.Validate(); err != nil {
				return util.BadRequest(err.Error())
			}
			if err := s.batchRepo.Save(txCtx, batch); err != nil {
				return err
			}
			request.Status = constants.PauseRequestApproved
			if err := s.audit.Record(txCtx, actor, "batch.transitioned", "ProductionBatch", batch.ID, batchBefore, batch); err != nil {
				return err
			}
		} else {
			request.Status = constants.PauseRequestRejected
		}
		request.Normalize()
		if err := request.Validate(); err != nil {
			return util.BadRequest(err.Error())
		}
		if err := s.repo.Save(txCtx, request); err != nil {
			return err
		}
		return s.audit.Record(txCtx, actor, "pause_request.reviewed", "PauseRequest", request.ID, before, request)
	})
	if err != nil {
		return nil, err
	}
	return s.repo.Find(ctx, request.ID)
}
