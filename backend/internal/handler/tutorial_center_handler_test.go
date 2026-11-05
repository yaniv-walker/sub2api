package handler

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type tutorialMediaRepo struct {
	service.TutorialRepository
	allowed bool
}

func (r *tutorialMediaRepo) CanReadAsset(context.Context, string, int64, int64, string) (bool, error) {
	return r.allowed, nil
}
func (r *tutorialMediaRepo) GetAssetByKey(context.Context, string) (*service.TutorialAsset, error) {
	return &service.TutorialAsset{StorageKey: "clip.mp4", Kind: "video", MIMEType: "video/mp4", OriginalName: "clip.mp4"}, nil
}
func TestTutorialMediaRangeRequiresScopedSignature(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{}
	cfg.Pricing.DataDir = t.TempDir()
	cfg.JWT.Secret = "test-secret"
	dir := filepath.Join(cfg.Pricing.DataDir, "tutorial-assets")
	require.NoError(t, os.MkdirAll(dir, 0750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "clip.mp4"), []byte("0123456789"), 0600))
	repo := &tutorialMediaRepo{allowed: true}
	h := NewTutorialCenterHandler(service.NewTutorialService(repo), nil, cfg)
	router := gin.New()
	router.GET("/api/v1/tutorial-assets/:token", h.Asset)
	unsigned := httptest.NewRecorder()
	router.ServeHTTP(unsigned, httptest.NewRequest("GET", "/api/v1/tutorial-assets/clip.mp4", nil))
	require.Equal(t, 403, unsigned.Code)
	signed := h.signer.URL("clip.mp4", 1, 3, "published", time.Now().Add(time.Hour))
	req := httptest.NewRequest("GET", signed, nil)
	req.Header.Set("Range", "bytes=2-5")
	result := httptest.NewRecorder()
	router.ServeHTTP(result, req)
	require.Equal(t, 206, result.Code)
	require.Equal(t, "2345", result.Body.String())
	require.Equal(t, "bytes 2-5/10", result.Header().Get("Content-Range"))
	require.Equal(t, "video/mp4", result.Header().Get("Content-Type"))
	repo.allowed = false
	denied := httptest.NewRecorder()
	router.ServeHTTP(denied, httptest.NewRequest("GET", signed, nil))
	require.Equal(t, 404, denied.Code)
}
