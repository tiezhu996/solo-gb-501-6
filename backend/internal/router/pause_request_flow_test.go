package router

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"sterile-packaging-release-control/backend/internal/config"
	"sterile-packaging-release-control/backend/internal/constants"
	"sterile-packaging-release-control/backend/internal/model"
	"sterile-packaging-release-control/backend/internal/util"
)

// 暂停复核全链路集成测试。通过真实 Gin 引擎和真实 PostgreSQL 运行，
// 每个用例自建产线/批次夹具并在结束后清理，可连续重复运行。
// 默认连接 sterile_release_test 库，可用 TEST_DATABASE_URL 覆盖；
// 测试库不可用时按失败处理，不允许用跳过掩盖环境缺口。

const pauseTestSecret = "pause-flow-test-secret-key-32chars!"

type pauseTestEnv struct {
	engine *gin.Engine
	db     *gorm.DB
	token  map[constants.Role]string
}

var (
	pauseEnvOnce sync.Once
	pauseEnv     *pauseTestEnv
	pauseEnvErr  error
)

func buildPauseTestEnv() (*pauseTestEnv, error) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://sterile:sterile_dev_password@localhost:5432/sterile_release_test?sslmode=disable"
	}
	db, err := util.OpenDatabase(dsn)
	if err != nil {
		return nil, fmt.Errorf("connect test database: %w", err)
	}
	if err := util.Migrate(db); err != nil {
		return nil, fmt.Errorf("migrate test database: %w", err)
	}
	cfg := config.Config{
		Environment: "test", Port: "0", JWTSecret: pauseTestSecret,
		TokenTTL: time.Hour, RateLimit: 100000, RateWindow: time.Minute,
	}
	redisClient := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	engine, err := Build(db, redisClient, cfg)
	if err != nil {
		return nil, fmt.Errorf("build router: %w", err)
	}
	env := &pauseTestEnv{engine: engine, db: db, token: map[constants.Role]string{}}
	for username, role := range map[string]constants.Role{
		"operator": constants.RoleOperator, "approver": constants.RoleApprover,
		"inspector": constants.RoleInspector, "admin": constants.RoleAdmin,
	} {
		var user model.User
		if err := db.Where("username = ?", username).First(&user).Error; err != nil {
			return nil, fmt.Errorf("find seeded user %s: %w", username, err)
		}
		token, _, err := util.SignToken(pauseTestSecret, time.Hour, user.ID, user.Username, user.DisplayName, user.Role)
		if err != nil {
			return nil, fmt.Errorf("sign token for %s: %w", username, err)
		}
		env.token[role] = token
	}
	return env, nil
}

func pauseEnvFor(t *testing.T) *pauseTestEnv {
	t.Helper()
	pauseEnvOnce.Do(func() { pauseEnv, pauseEnvErr = buildPauseTestEnv() })
	if pauseEnvErr != nil {
		t.Fatalf("暂停复核集成测试环境不可用，按失败处理: %v", pauseEnvErr)
	}
	return pauseEnv
}

// pauseFixture 是一条产线加一个批次，测试结束即整体清理。
type pauseFixture struct {
	line  *model.PackagingLine
	batch *model.ProductionBatch
}

func (e *pauseTestEnv) newFixture(t *testing.T, status constants.BatchStatus) *pauseFixture {
	t.Helper()
	suffix := fmt.Sprint(time.Now().UnixNano())
	line := &model.PackagingLine{
		Code: "PTL" + suffix, Name: "暂停复核测试产线", Team: "测试班组",
		EquipmentStatus: "running", Location: "测试区", Active: true,
	}
	if err := e.db.Create(line).Error; err != nil {
		t.Fatalf("create fixture line: %v", err)
	}
	batch := &model.ProductionBatch{
		BatchNo: "PTB" + suffix, Specification: "暂停复核测试规格", Status: status,
		ResponsibleTeam: "测试班组", PackagingLineID: line.ID,
		PlannedQuantity: 100, ProducedQuantity: 40,
	}
	if status == constants.BatchStatusHold {
		batch.HoldReason = "夹具初始暂停"
	}
	if err := e.db.Create(batch).Error; err != nil {
		t.Fatalf("create fixture batch: %v", err)
	}
	fixture := &pauseFixture{line: line, batch: batch}
	t.Cleanup(func() {
		e.db.Exec(`DELETE FROM audit_logs WHERE
			(entity_type = 'ProductionBatch' AND entity_id = ?) OR
			(entity_type = 'PauseRequest' AND entity_id IN (SELECT id FROM pause_requests WHERE production_batch_id = ?)) OR
			(entity_type = 'InspectionSample' AND entity_id IN (SELECT id FROM inspection_samples WHERE production_batch_id = ?))`,
			batch.ID, batch.ID, batch.ID)
		e.db.Exec("DELETE FROM inspection_samples WHERE production_batch_id = ?", batch.ID)
		e.db.Exec("DELETE FROM release_decisions WHERE production_batch_id = ?", batch.ID)
		e.db.Exec("DELETE FROM pause_requests WHERE production_batch_id = ?", batch.ID)
		e.db.Exec("DELETE FROM production_batches WHERE id = ?", batch.ID)
		e.db.Exec("DELETE FROM packaging_lines WHERE id = ?", line.ID)
	})
	return fixture
}

func (e *pauseTestEnv) call(t *testing.T, method, path string, role constants.Role, body any) (int, map[string]any) {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
		reader = bytes.NewReader(payload)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	if role != "" {
		req.Header.Set("Authorization", "Bearer "+e.token[role])
	}
	recorder := httptest.NewRecorder()
	e.engine.ServeHTTP(recorder, req)
	var parsed map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("parse response %d %s: %v", recorder.Code, recorder.Body.String(), err)
	}
	return recorder.Code, parsed
}

func dataObj(t *testing.T, resp map[string]any) map[string]any {
	t.Helper()
	data, ok := resp["data"].(map[string]any)
	if !ok {
		t.Fatalf("response has no data object: %v", resp)
	}
	return data
}

func errorMessage(t *testing.T, resp map[string]any) string {
	t.Helper()
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("response has no error object: %v", resp)
	}
	message, _ := errObj["message"].(string)
	return message
}

func (e *pauseTestEnv) batchStatus(t *testing.T, id uint) (string, string) {
	t.Helper()
	status, resp := e.call(t, http.MethodGet, fmt.Sprintf("/api/batches/%d", id), constants.RoleOperator, nil)
	if status != http.StatusOK {
		t.Fatalf("get batch %d returned %d", id, status)
	}
	data := dataObj(t, resp)
	batchStatus, _ := data["status"].(string)
	holdReason, _ := data["holdReason"].(string)
	return batchStatus, holdReason
}

func (e *pauseTestEnv) pauseRequestState(t *testing.T, id uint) map[string]any {
	t.Helper()
	status, resp := e.call(t, http.MethodGet, fmt.Sprintf("/api/pause-requests/%d", id), constants.RoleApprover, nil)
	if status != http.StatusOK {
		t.Fatalf("get pause request %d returned %d", id, status)
	}
	return dataObj(t, resp)
}

func (e *pauseTestEnv) applyPause(t *testing.T, batchID uint, reason string) uint {
	t.Helper()
	status, resp := e.call(t, http.MethodPost, fmt.Sprintf("/api/batches/%d/pause-requests", batchID), constants.RoleOperator, map[string]any{"reason": reason})
	if status != http.StatusCreated {
		t.Fatalf("apply pause returned %d: %v", status, resp)
	}
	return uint(dataObj(t, resp)["id"].(float64))
}

func (e *pauseTestEnv) pendingCount(t *testing.T, batchID uint) int64 {
	t.Helper()
	var count int64
	if err := e.db.Model(&model.PauseRequest{}).
		Where("production_batch_id = ? AND status = ?", batchID, constants.PauseRequestPending).
		Count(&count).Error; err != nil {
		t.Fatalf("count pending pause requests: %v", err)
	}
	return count
}

func TestPauseRequestSubmit(t *testing.T) {
	env := pauseEnvFor(t)
	fixture := env.newFixture(t, constants.BatchStatusRunning)

	status, resp := env.call(t, http.MethodPost, fmt.Sprintf("/api/batches/%d/pause-requests", fixture.batch.ID), constants.RoleOperator, map[string]any{"reason": "热封刀温度漂移，需要停机校准"})
	if status != http.StatusCreated {
		t.Fatalf("submit returned %d, want 201: %v", status, resp)
	}
	created := dataObj(t, resp)
	if created["status"] != string(constants.PauseRequestPending) {
		t.Fatalf("new request status = %v, want pending", created["status"])
	}
	if created["applicantName"] != "产线操作员" {
		t.Fatalf("applicant = %v, want 产线操作员", created["applicantName"])
	}
	if created["reason"] != "热封刀温度漂移，需要停机校准" {
		t.Fatalf("reason = %v", created["reason"])
	}
	if batchStatus, _ := env.batchStatus(t, fixture.batch.ID); batchStatus != string(constants.BatchStatusRunning) {
		t.Fatalf("batch status after submit = %s, want running", batchStatus)
	}

	// 原因过短被参数校验拒绝
	status, resp = env.call(t, http.MethodPost, fmt.Sprintf("/api/batches/%d/pause-requests", fixture.batch.ID), constants.RoleOperator, map[string]any{"reason": "停"})
	if status != http.StatusBadRequest {
		t.Fatalf("short reason returned %d, want 400: %v", status, resp)
	}

	// 非 running 批次不能申请
	draftFixture := env.newFixture(t, constants.BatchStatusDraft)
	status, resp = env.call(t, http.MethodPost, fmt.Sprintf("/api/batches/%d/pause-requests", draftFixture.batch.ID), constants.RoleOperator, map[string]any{"reason": "草稿批次申请暂停"})
	if status != http.StatusConflict {
		t.Fatalf("draft batch apply returned %d, want 409: %v", status, resp)
	}
	if got := errorMessage(t, resp); got != "只有生产中的批次才能申请暂停" {
		t.Fatalf("message = %q", got)
	}
}

func TestPauseRequestApprove(t *testing.T) {
	env := pauseEnvFor(t)
	fixture := env.newFixture(t, constants.BatchStatusRunning)
	requestID := env.applyPause(t, fixture.batch.ID, "模具磨损超标，需停机更换")

	status, resp := env.call(t, http.MethodPost, fmt.Sprintf("/api/pause-requests/%d/review", requestID), constants.RoleApprover, map[string]any{"action": "approve", "comment": "同意停机，更换后复机需复检"})
	if status != http.StatusOK {
		t.Fatalf("approve returned %d, want 200: %v", status, resp)
	}
	state := env.pauseRequestState(t, requestID)
	if state["status"] != string(constants.PauseRequestApproved) {
		t.Fatalf("request status = %v, want approved", state["status"])
	}
	if state["reviewerName"] != "放行审批员" {
		t.Fatalf("reviewer = %v, want 放行审批员", state["reviewerName"])
	}
	if state["reviewComment"] != "同意停机，更换后复机需复检" {
		t.Fatalf("review comment = %v", state["reviewComment"])
	}
	if state["reviewedAt"] == nil || state["reviewedAt"] == "" {
		t.Fatal("reviewedAt must be set after approval")
	}
	batchStatus, holdReason := env.batchStatus(t, fixture.batch.ID)
	if batchStatus != string(constants.BatchStatusHold) {
		t.Fatalf("batch status after approve = %s, want hold", batchStatus)
	}
	if holdReason != "模具磨损超标，需停机更换" {
		t.Fatalf("hold reason = %q, want 申请原因", holdReason)
	}

	// 审批动作写入审计：申请、审批、批次变更各一条
	status, resp = env.call(t, http.MethodGet, fmt.Sprintf("/api/audit-logs?entityType=PauseRequest&pageSize=100"), constants.RoleAdmin, nil)
	if status != http.StatusOK {
		t.Fatalf("list audit logs returned %d", status)
	}
	actions := map[string]bool{}
	for _, item := range dataObj(t, resp)["items"].([]any) {
		entry := item.(map[string]any)
		if uint(entry["entityId"].(float64)) == requestID {
			actions[entry["action"].(string)] = true
		}
	}
	if !actions["pause_request.created"] || !actions["pause_request.reviewed"] {
		t.Fatalf("audit actions for request %d = %v", requestID, actions)
	}
}

func TestPauseRequestReject(t *testing.T) {
	env := pauseEnvFor(t)
	fixture := env.newFixture(t, constants.BatchStatusRunning)
	requestID := env.applyPause(t, fixture.batch.ID, "计划停机保养")

	// 拒绝必须填写结论
	status, resp := env.call(t, http.MethodPost, fmt.Sprintf("/api/pause-requests/%d/review", requestID), constants.RoleApprover, map[string]any{"action": "reject", "comment": ""})
	if status != http.StatusBadRequest {
		t.Fatalf("reject without comment returned %d, want 400: %v", status, resp)
	}

	status, resp = env.call(t, http.MethodPost, fmt.Sprintf("/api/pause-requests/%d/review", requestID), constants.RoleApprover, map[string]any{"action": "reject", "comment": "订单交期紧张，完成本批后再停机"})
	if status != http.StatusOK {
		t.Fatalf("reject returned %d, want 200: %v", status, resp)
	}
	state := env.pauseRequestState(t, requestID)
	if state["status"] != string(constants.PauseRequestRejected) {
		t.Fatalf("request status = %v, want rejected", state["status"])
	}
	if state["reviewComment"] != "订单交期紧张，完成本批后再停机" {
		t.Fatalf("reject conclusion = %v", state["reviewComment"])
	}
	if batchStatus, _ := env.batchStatus(t, fixture.batch.ID); batchStatus != string(constants.BatchStatusRunning) {
		t.Fatalf("batch status after reject = %s, want running", batchStatus)
	}

	// 拒绝后阻断解除，可以重新登记检验
	status, resp = env.call(t, http.MethodPost, "/api/inspections", constants.RoleInspector, map[string]any{
		"productionBatchId": fixture.batch.ID, "sampleCode": "S-PT" + fmt.Sprint(time.Now().UnixNano()),
		"samplingPosition": "批次中段", "inspectionItem": "热封强度", "acceptanceRange": ">= 1.50 N/15mm",
	})
	if status != http.StatusCreated {
		t.Fatalf("inspection after reject returned %d, want 201: %v", status, resp)
	}

	// 已处理的申请不能重复审批
	status, resp = env.call(t, http.MethodPost, fmt.Sprintf("/api/pause-requests/%d/review", requestID), constants.RoleApprover, map[string]any{"action": "approve"})
	if status != http.StatusConflict {
		t.Fatalf("re-review returned %d, want 409: %v", status, resp)
	}
}

func TestPauseRequestDuplicate(t *testing.T) {
	env := pauseEnvFor(t)
	fixture := env.newFixture(t, constants.BatchStatusRunning)
	env.applyPause(t, fixture.batch.ID, "第一份暂停申请")

	status, resp := env.call(t, http.MethodPost, fmt.Sprintf("/api/batches/%d/pause-requests", fixture.batch.ID), constants.RoleOperator, map[string]any{"reason": "第二份暂停申请"})
	if status != http.StatusConflict {
		t.Fatalf("duplicate apply returned %d, want 409: %v", status, resp)
	}
	if got := errorMessage(t, resp); got != "该批次已有待处理的暂停申请" {
		t.Fatalf("message = %q", got)
	}
	if count := env.pendingCount(t, fixture.batch.ID); count != 1 {
		t.Fatalf("pending requests = %d, want 1", count)
	}
}

func TestPauseRequestPendingBlocks(t *testing.T) {
	env := pauseEnvFor(t)
	fixture := env.newFixture(t, constants.BatchStatusRunning)
	env.applyPause(t, fixture.batch.ID, "等待审批期间验证阻断")

	status, resp := env.call(t, http.MethodPost, "/api/inspections", constants.RoleInspector, map[string]any{
		"productionBatchId": fixture.batch.ID, "sampleCode": "S-PT" + fmt.Sprint(time.Now().UnixNano()),
		"samplingPosition": "批次起始段", "inspectionItem": "染色渗透", "acceptanceRange": "封边无连续通道",
	})
	if status != http.StatusConflict {
		t.Fatalf("inspection during pending returned %d, want 409: %v", status, resp)
	}
	if got := errorMessage(t, resp); got != "批次存在待处理的暂停申请，不能新增检验" {
		t.Fatalf("inspection block message = %q", got)
	}

	status, resp = env.call(t, http.MethodPost, "/api/release-decisions", constants.RoleApprover, map[string]any{
		"productionBatchId": fixture.batch.ID, "decision": "release", "reason": "检验全部合格，申请放行",
	})
	if status != http.StatusConflict {
		t.Fatalf("release during pending returned %d, want 409: %v", status, resp)
	}
	if got := errorMessage(t, resp); got != "批次存在待处理的暂停申请，不能提交放行决定" {
		t.Fatalf("release block message = %q", got)
	}
}

func TestPauseRequestStateDrift(t *testing.T) {
	env := pauseEnvFor(t)
	fixture := env.newFixture(t, constants.BatchStatusRunning)
	requestID := env.applyPause(t, fixture.batch.ID, "申请后批次状态漂移")

	// 申请提交后批次被转入返工
	status, resp := env.call(t, http.MethodPost, fmt.Sprintf("/api/batches/%d/transition", fixture.batch.ID), constants.RoleOperator, map[string]any{"status": "rework"})
	if status != http.StatusOK {
		t.Fatalf("transition to rework returned %d: %v", status, resp)
	}

	// 审批时批次已不是 running，同意必须失败且申请保持待处理
	status, resp = env.call(t, http.MethodPost, fmt.Sprintf("/api/pause-requests/%d/review", requestID), constants.RoleApprover, map[string]any{"action": "approve"})
	if status != http.StatusConflict {
		t.Fatalf("approve after drift returned %d, want 409: %v", status, resp)
	}
	if got := errorMessage(t, resp); got != "批次状态已变化，无法执行暂停" {
		t.Fatalf("drift message = %q", got)
	}
	if state := env.pauseRequestState(t, requestID); state["status"] != string(constants.PauseRequestPending) {
		t.Fatalf("request status after failed approve = %v, want pending", state["status"])
	}
	if batchStatus, _ := env.batchStatus(t, fixture.batch.ID); batchStatus != string(constants.BatchStatusRework) {
		t.Fatalf("batch status = %s, want rework", batchStatus)
	}
}

func TestPauseRequestForbidden(t *testing.T) {
	env := pauseEnvFor(t)
	fixture := env.newFixture(t, constants.BatchStatusRunning)

	// 检验员和审批员都没有 batch:write，不能提交暂停申请
	for _, role := range []constants.Role{constants.RoleInspector, constants.RoleApprover} {
		status, _ := env.call(t, http.MethodPost, fmt.Sprintf("/api/batches/%d/pause-requests", fixture.batch.ID), role, map[string]any{"reason": "越权提交暂停申请"})
		if status != http.StatusForbidden {
			t.Fatalf("apply as %s returned %d, want 403", role, status)
		}
	}

	// 操作员没有 release:write，不能审批
	requestID := env.applyPause(t, fixture.batch.ID, "等待越权审批")
	status, _ := env.call(t, http.MethodPost, fmt.Sprintf("/api/pause-requests/%d/review", requestID), constants.RoleOperator, map[string]any{"action": "approve"})
	if status != http.StatusForbidden {
		t.Fatalf("review as operator returned %d, want 403", status)
	}
	if state := env.pauseRequestState(t, requestID); state["status"] != string(constants.PauseRequestPending) {
		t.Fatalf("request status after forbidden review = %v, want pending", state["status"])
	}

	// 直接暂停入口已取消
	status, resp := env.call(t, http.MethodPost, fmt.Sprintf("/api/batches/%d/transition", fixture.batch.ID), constants.RoleOperator, map[string]any{"status": "hold", "reason": "试图直接暂停"})
	if status != http.StatusForbidden {
		t.Fatalf("direct hold transition returned %d, want 403: %v", status, resp)
	}
	if got := errorMessage(t, resp); got != "暂停批次需提交暂停申请并由审批人员同意" {
		t.Fatalf("direct hold message = %q", got)
	}
}

func TestPauseRequestUnauthenticated(t *testing.T) {
	env := pauseEnvFor(t)
	fixture := env.newFixture(t, constants.BatchStatusRunning)

	for _, target := range []struct{ method, path string }{
		{http.MethodPost, fmt.Sprintf("/api/batches/%d/pause-requests", fixture.batch.ID)},
		{http.MethodPost, "/api/pause-requests/1/review"},
		{http.MethodPost, "/api/pause-requests/1/withdraw"},
		{http.MethodGet, "/api/pause-requests"},
	} {
		status, _ := env.call(t, target.method, target.path, "", map[string]any{"reason": "未登录访问", "action": "approve"})
		if status != http.StatusUnauthorized {
			t.Fatalf("%s %s without token returned %d, want 401", target.method, target.path, status)
		}
	}
}

func TestPauseRequestConcurrentDuplicate(t *testing.T) {
	env := pauseEnvFor(t)
	fixture := env.newFixture(t, constants.BatchStatusRunning)

	const workers = 8
	codes := make(chan int, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			status, _ := env.call(t, http.MethodPost, fmt.Sprintf("/api/batches/%d/pause-requests", fixture.batch.ID), constants.RoleOperator, map[string]any{"reason": fmt.Sprintf("并发申请 %d 号停机", i)})
			codes <- status
		}(i)
	}
	wg.Wait()
	close(codes)

	created, conflicts, other := 0, 0, 0
	for code := range codes {
		switch code {
		case http.StatusCreated:
			created++
		case http.StatusConflict:
			conflicts++
		default:
			other++
		}
	}
	if created != 1 || conflicts != workers-1 || other != 0 {
		t.Fatalf("concurrent apply: created=%d conflicts=%d other=%d, want 1/%d/0", created, conflicts, other, workers-1)
	}
	if count := env.pendingCount(t, fixture.batch.ID); count != 1 {
		t.Fatalf("pending requests after concurrent apply = %d, want 1", count)
	}
}

func TestPauseRequestWithdraw(t *testing.T) {
	env := pauseEnvFor(t)
	fixture := env.newFixture(t, constants.BatchStatusRunning)
	requestID := env.applyPause(t, fixture.batch.ID, "误报的停机申请")

	status, resp := env.call(t, http.MethodPost, fmt.Sprintf("/api/pause-requests/%d/withdraw", requestID), constants.RoleOperator, nil)
	if status != http.StatusOK {
		t.Fatalf("withdraw returned %d, want 200: %v", status, resp)
	}
	state := env.pauseRequestState(t, requestID)
	if state["status"] != string(constants.PauseRequestWithdrawn) {
		t.Fatalf("request status = %v, want withdrawn", state["status"])
	}
	if state["reviewerName"] != "产线操作员" {
		t.Fatalf("withdrawer = %v, want 产线操作员", state["reviewerName"])
	}
	if state["reviewedAt"] == nil || state["reviewedAt"] == "" {
		t.Fatal("reviewedAt must be set after withdraw")
	}
	if batchStatus, _ := env.batchStatus(t, fixture.batch.ID); batchStatus != string(constants.BatchStatusRunning) {
		t.Fatalf("batch status after withdraw = %s, want running", batchStatus)
	}

	// 撤销后阻断解除，可以登记检验
	status, resp = env.call(t, http.MethodPost, "/api/inspections", constants.RoleInspector, map[string]any{
		"productionBatchId": fixture.batch.ID, "sampleCode": "S-PT" + fmt.Sprint(time.Now().UnixNano()),
		"samplingPosition": "批次末段", "inspectionItem": "外观完整性", "acceptanceRange": "外观无可见缺陷",
	})
	if status != http.StatusCreated {
		t.Fatalf("inspection after withdraw returned %d, want 201: %v", status, resp)
	}

	// 撤销不占用待处理名额，可以重新申请
	secondID := env.applyPause(t, fixture.batch.ID, "重新评估后仍需停机")
	if count := env.pendingCount(t, fixture.batch.ID); count != 1 {
		t.Fatalf("pending requests after re-apply = %d, want 1", count)
	}

	// 已撤销的申请不能再次撤销
	status, resp = env.call(t, http.MethodPost, fmt.Sprintf("/api/pause-requests/%d/withdraw", requestID), constants.RoleOperator, nil)
	if status != http.StatusConflict {
		t.Fatalf("re-withdraw returned %d, want 409: %v", status, resp)
	}
	if got := errorMessage(t, resp); got != "该暂停申请已处理，不能撤销" {
		t.Fatalf("re-withdraw message = %q", got)
	}

	// 已审批的申请同样不能撤销
	status, resp = env.call(t, http.MethodPost, fmt.Sprintf("/api/pause-requests/%d/review", secondID), constants.RoleApprover, map[string]any{"action": "approve", "comment": "同意停机"})
	if status != http.StatusOK {
		t.Fatalf("approve second request returned %d: %v", status, resp)
	}
	status, resp = env.call(t, http.MethodPost, fmt.Sprintf("/api/pause-requests/%d/withdraw", secondID), constants.RoleOperator, nil)
	if status != http.StatusConflict {
		t.Fatalf("withdraw approved request returned %d, want 409: %v", status, resp)
	}

	// 撤销动作写入审计
	status, resp = env.call(t, http.MethodGet, "/api/audit-logs?entityType=PauseRequest&pageSize=100", constants.RoleAdmin, nil)
	if status != http.StatusOK {
		t.Fatalf("list audit logs returned %d", status)
	}
	found := false
	for _, item := range dataObj(t, resp)["items"].([]any) {
		entry := item.(map[string]any)
		if uint(entry["entityId"].(float64)) == requestID && entry["action"] == "pause_request.withdrawn" {
			found = true
		}
	}
	if !found {
		t.Fatalf("audit log for withdrawn request %d not found", requestID)
	}
}

func TestPauseRequestWithdrawForbidden(t *testing.T) {
	env := pauseEnvFor(t)
	fixture := env.newFixture(t, constants.BatchStatusRunning)
	requestID := env.applyPause(t, fixture.batch.ID, "等待他人尝试撤销")

	// 非申请人即使有 batch:write 也不能撤销
	status, resp := env.call(t, http.MethodPost, fmt.Sprintf("/api/pause-requests/%d/withdraw", requestID), constants.RoleAdmin, nil)
	if status != http.StatusForbidden {
		t.Fatalf("withdraw as non-applicant admin returned %d, want 403: %v", status, resp)
	}
	if got := errorMessage(t, resp); got != "只有申请人本人可以撤销暂停申请" {
		t.Fatalf("non-applicant message = %q", got)
	}

	// 没有 batch:write 的角色直接被 RBAC 拦截
	for _, role := range []constants.Role{constants.RoleApprover, constants.RoleInspector} {
		status, _ := env.call(t, http.MethodPost, fmt.Sprintf("/api/pause-requests/%d/withdraw", requestID), role, nil)
		if status != http.StatusForbidden {
			t.Fatalf("withdraw as %s returned %d, want 403", role, status)
		}
	}

	if state := env.pauseRequestState(t, requestID); state["status"] != string(constants.PauseRequestPending) {
		t.Fatalf("request status after forbidden withdraws = %v, want pending", state["status"])
	}
}

func TestPauseRequestConcurrentWithdrawReview(t *testing.T) {
	env := pauseEnvFor(t)
	fixture := env.newFixture(t, constants.BatchStatusRunning)
	requestID := env.applyPause(t, fixture.batch.ID, "并发撤销与审批")

	type outcome struct {
		name string
		code int
	}
	results := make(chan outcome, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		status, _ := env.call(t, http.MethodPost, fmt.Sprintf("/api/pause-requests/%d/withdraw", requestID), constants.RoleOperator, nil)
		results <- outcome{"withdraw", status}
	}()
	go func() {
		defer wg.Done()
		status, _ := env.call(t, http.MethodPost, fmt.Sprintf("/api/pause-requests/%d/review", requestID), constants.RoleApprover, map[string]any{"action": "approve"})
		results <- outcome{"approve", status}
	}()
	wg.Wait()
	close(results)

	codes := map[string]int{}
	for result := range results {
		codes[result.name] = result.code
	}
	okCount := 0
	for _, code := range codes {
		if code == http.StatusOK {
			okCount++
		} else if code != http.StatusConflict {
			t.Fatalf("unexpected status codes: %v", codes)
		}
	}
	if okCount != 1 {
		t.Fatalf("concurrent withdraw/review: exactly one must succeed, got %v", codes)
	}

	// 最终状态必须自洽：撤销则批次保持运行，同意则批次进入暂停
	state := env.pauseRequestState(t, requestID)
	batchStatus, _ := env.batchStatus(t, fixture.batch.ID)
	switch state["status"] {
	case string(constants.PauseRequestWithdrawn):
		if codes["withdraw"] != http.StatusOK || batchStatus != string(constants.BatchStatusRunning) {
			t.Fatalf("withdraw won but codes=%v batch=%s", codes, batchStatus)
		}
	case string(constants.PauseRequestApproved):
		if codes["approve"] != http.StatusOK || batchStatus != string(constants.BatchStatusHold) {
			t.Fatalf("approve won but codes=%v batch=%s", codes, batchStatus)
		}
	default:
		t.Fatalf("request status = %v, want withdrawn or approved", state["status"])
	}
}
