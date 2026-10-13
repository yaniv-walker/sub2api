package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type CheckinHandler struct{ service *service.CheckinService }

func NewCheckinHandler(s *service.CheckinService) *CheckinHandler { return &CheckinHandler{service: s} }

func checkinUser(c *gin.Context) (int64, bool) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "User not authenticated")
		return 0, false
	}
	return subject.UserID, true
}

func (h *CheckinHandler) Status(c *gin.Context) {
	id, ok := checkinUser(c)
	if !ok {
		return
	}
	result, err := h.service.Status(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *CheckinHandler) Cycle(c *gin.Context) {
	id, ok := checkinUser(c)
	if !ok {
		return
	}
	result, err := h.service.Status(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result.Cycle)
}

func (h *CheckinHandler) Claim(c *gin.Context) {
	id, ok := checkinUser(c)
	if !ok {
		return
	}
	// Never accept client-selected amounts, dates, users or random results.
	if c.Request.Body != nil {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1024)
		decoder := json.NewDecoder(c.Request.Body)
		var body map[string]json.RawMessage
		err := decoder.Decode(&body)
		if err != nil && err != io.EOF {
			response.BadRequest(c, "签到请求必须为空对象")
			return
		}
		if len(body) != 0 || (err == nil && body == nil) {
			response.BadRequest(c, "签到请求不接受用户、金额或日期参数")
			return
		}
		if err == nil {
			var trailing any
			if decoder.Decode(&trailing) != io.EOF {
				response.BadRequest(c, "Invalid request body")
				return
			}
		}
	}
	result, err := h.service.Claim(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *CheckinHandler) History(c *gin.Context) {
	id, ok := checkinUser(c)
	if !ok {
		return
	}
	cursor, limit, ok := checkinPagination(c)
	if !ok {
		return
	}
	result, err := h.service.History(c.Request.Context(), id, cursor, limit)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func checkinPagination(c *gin.Context) (int64, int, bool) {
	cursor, err := strconv.ParseInt(c.DefaultQuery("cursor", "0"), 10, 64)
	limit, limitErr := strconv.Atoi(c.DefaultQuery("limit", "20"))
	if err != nil || limitErr != nil || cursor < 0 || limit < 1 || limit > 100 {
		response.BadRequest(c, "Invalid pagination")
		return 0, 0, false
	}
	return cursor, limit, true
}

// These handlers are registered only inside the existing admin-authenticated group.
func (h *CheckinHandler) AdminConfig(c *gin.Context) {
	result, err := h.service.Config(c.Request.Context(), true)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *CheckinHandler) AdminUpdateConfig(c *gin.Context) {
	id, ok := checkinUser(c)
	if !ok {
		return
	}
	var request struct {
		ExpectedVersion *int64                `json:"expected_version"`
		Config          service.CheckinConfig `json:"config"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64*1024)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil || request.ExpectedVersion == nil {
		response.BadRequest(c, "Invalid versioned checkin configuration")
		return
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		response.BadRequest(c, "Invalid request body")
		return
	}
	result, err := h.service.UpdateConfig(c.Request.Context(), request.Config, *request.ExpectedVersion, id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *CheckinHandler) AdminHistory(c *gin.Context) {
	cursor, limit, ok := checkinPagination(c)
	if !ok {
		return
	}
	userID, err := strconv.ParseInt(c.DefaultQuery("user_id", "0"), 10, 64)
	if err != nil || userID < 0 {
		response.BadRequest(c, "Invalid user_id")
		return
	}
	result, err := h.service.RewardHistory(c.Request.Context(), service.CheckinHistoryFilter{UserID: userID, Cursor: cursor, Limit: limit, DateFrom: c.Query("date_from"), DateTo: c.Query("date_to"), RewardType: c.Query("reward_type")})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *CheckinHandler) AdminBudgets(c *gin.Context) {
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "30"))
	if err != nil {
		response.BadRequest(c, "Invalid limit")
		return
	}
	result, err := h.service.Budgets(c.Request.Context(), c.Query("date_from"), c.Query("date_to"), limit)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *CheckinHandler) AdminCycles(c *gin.Context) {
	cursor, limit, ok := checkinPagination(c)
	if !ok {
		return
	}
	userID, err := strconv.ParseInt(c.DefaultQuery("user_id", "0"), 10, 64)
	if err != nil || userID < 0 {
		response.BadRequest(c, "Invalid user_id")
		return
	}
	result, err := h.service.Cycles(c.Request.Context(), userID, cursor, limit)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *CheckinHandler) AdminResolveCycle(c *gin.Context) {
	actor, ok := checkinUser(c)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid settlement id")
		return
	}
	var request struct {
		Action string `json:"action"`
		Reason string `json:"reason"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		response.BadRequest(c, "Invalid resolution request")
		return
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		response.BadRequest(c, "Invalid request body")
		return
	}
	if err := h.service.ResolveCycle(c.Request.Context(), id, actor, request.Action, request.Reason); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"resolved": true})
}
