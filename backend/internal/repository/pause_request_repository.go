package repository

import (
	"context"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"sterile-packaging-release-control/backend/internal/constants"
	"sterile-packaging-release-control/backend/internal/dto"
	"sterile-packaging-release-control/backend/internal/model"
)

type PauseRequestFilter struct {
	dto.PageQuery
	Status  string `form:"status"`
	BatchID uint   `form:"batchId"`
}

type PauseRequestRepository interface {
	List(context.Context, PauseRequestFilter) ([]model.PauseRequest, int64, error)
	Find(context.Context, uint) (*model.PauseRequest, error)
	FindForUpdate(context.Context, uint) (*model.PauseRequest, error)
	CountPendingForBatch(context.Context, uint) (int64, error)
	Create(context.Context, *model.PauseRequest) error
	Save(context.Context, *model.PauseRequest) error
}

type pauseRequestRepository struct{ db *gorm.DB }

func NewPauseRequestRepository(db *gorm.DB) PauseRequestRepository {
	return &pauseRequestRepository{db: db}
}

func (r *pauseRequestRepository) List(ctx context.Context, filter PauseRequestFilter) ([]model.PauseRequest, int64, error) {
	query := filter.PageQuery.Normalize()
	db := dbForContext(ctx, r.db).Model(&model.PauseRequest{})
	if filter.Status != "" {
		db = db.Where("status = ?", filter.Status)
	}
	if filter.BatchID > 0 {
		db = db.Where("production_batch_id = ?", filter.BatchID)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var requests []model.PauseRequest
	err := db.Preload("ProductionBatch").Order("created_at DESC").Offset((query.Page - 1) * query.PageSize).Limit(query.PageSize).Find(&requests).Error
	return requests, total, err
}

func (r *pauseRequestRepository) Find(ctx context.Context, id uint) (*model.PauseRequest, error) {
	var request model.PauseRequest
	err := dbForContext(ctx, r.db).Preload("ProductionBatch").First(&request, id).Error
	return &request, err
}

func (r *pauseRequestRepository) FindForUpdate(ctx context.Context, id uint) (*model.PauseRequest, error) {
	var request model.PauseRequest
	err := dbForContext(ctx, r.db).Clauses(clause.Locking{Strength: "UPDATE"}).
		Preload("ProductionBatch").First(&request, id).Error
	return &request, err
}

func (r *pauseRequestRepository) CountPendingForBatch(ctx context.Context, batchID uint) (int64, error) {
	var count int64
	err := dbForContext(ctx, r.db).Model(&model.PauseRequest{}).
		Where("production_batch_id = ? AND status = ?", batchID, constants.PauseRequestPending).Count(&count).Error
	return count, err
}

func (r *pauseRequestRepository) Create(ctx context.Context, request *model.PauseRequest) error {
	return dbForContext(ctx, r.db).Create(request).Error
}

func (r *pauseRequestRepository) Save(ctx context.Context, request *model.PauseRequest) error {
	return dbForContext(ctx, r.db).Save(request).Error
}
