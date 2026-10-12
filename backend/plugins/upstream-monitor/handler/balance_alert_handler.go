package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/Wei-Shaw/sub2api/plugins/upstream-monitor/model"
	"github.com/Wei-Shaw/sub2api/plugins/upstream-monitor/repository"
	"github.com/Wei-Shaw/sub2api/plugins/upstream-monitor/service"
	"github.com/gin-gonic/gin"
)

type BalanceAlertConfigStore interface {
	Get(context.Context, int64) (*model.BalanceAlertRule, error)
	Save(context.Context, int64, model.BalanceAlertConfig) error
}
type BalanceAlertHandler struct {
	store     BalanceAlertConfigStore
	upstreams UpstreamProvider
}

func NewBalanceAlertHandler(store BalanceAlertConfigStore, upstreams UpstreamProvider) *BalanceAlertHandler {
	return &BalanceAlertHandler{store: store, upstreams: upstreams}
}
func (h *BalanceAlertHandler) RegisterRoutes(router *gin.RouterGroup) {
	router.GET("/upstreams/:id/balance-alert", h.Get)
	router.PUT("/upstreams/:id/balance-alert", h.Save)
}
func (h *BalanceAlertHandler) upstreamID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(400, gin.H{"error": "invalid upstream id"})
		return 0, false
	}
	configs, err := h.upstreams.List(c.Request.Context())
	if err != nil {
		c.JSON(500, gin.H{"error": "unable to load upstream configurations"})
		return 0, false
	}
	for _, u := range configs {
		if u.ID == id {
			return id, true
		}
	}
	c.JSON(404, gin.H{"error": "upstream not found"})
	return 0, false
}
func (h *BalanceAlertHandler) Get(c *gin.Context) {
	id, ok := h.upstreamID(c)
	if !ok {
		return
	}
	rule, err := h.store.Get(c.Request.Context(), id)
	if err != nil {
		c.JSON(500, gin.H{"error": "unable to load balance alert"})
		return
	}
	c.JSON(http.StatusOK, rule)
}
func (h *BalanceAlertHandler) Save(c *gin.Context) {
	id, ok := h.upstreamID(c)
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8192)
	var request model.BalanceAlertConfig
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(400, gin.H{"error": "invalid balance alert configuration"})
		return
	}
	if err := request.Validate(); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if err := h.store.Save(c.Request.Context(), id, request); err != nil {
		if errors.Is(err, repository.ErrAlertBusy) {
			c.JSON(409, gin.H{"error": err.Error()})
			return
		}
		c.JSON(500, gin.H{"error": "unable to save balance alert"})
		return
	}
	rule, err := h.store.Get(c.Request.Context(), id)
	if err != nil {
		c.JSON(500, gin.H{"error": "balance alert saved; unable to reload configuration"})
		return
	}
	c.JSON(200, rule)
}

// PrepareAlertBalances groups accounts and decrypts credentials once per batch.
func (h *MonitorHandler) PrepareAlertBalances(ctx context.Context) (service.BalanceAlertReadFunc, error) {
	upstreams, err := h.loadMonitorUpstreams(ctx)
	if err != nil {
		return nil, err
	}
	targets := make(map[int64]monitorUpstream, len(upstreams))
	for _, u := range upstreams {
		if u.ID <= 0 || !u.Enabled || u.CredentialError != "" || len(u.Accounts) == 0 {
			continue
		}
		targets[u.ID] = u
	}
	return func(ctx context.Context, id int64) (service.BalanceAlertSample, error) {
		u, ok := targets[id]
		if !ok {
			return service.BalanceAlertSample{}, fmt.Errorf("upstream is disabled or unavailable")
		}
		account := u.Accounts[0].info
		info, err := h.fetcher.FetchInfoContext(ctx, account.ID, account.UpstreamType, account.BaseURL, account.ApiKey, account.QuotaDivider)
		if err != nil {
			return service.BalanceAlertSample{}, err
		}
		if !info.BalanceKnown {
			return service.BalanceAlertSample{}, fmt.Errorf("upstream balance is unknown")
		}
		return service.BalanceAlertSample{Name: u.Name, URL: u.BaseURL, Type: u.Type, Balance: info.Balance}, nil
	}, nil
}
