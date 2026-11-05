package admin

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type tutorialUploadRepo struct {
	service.TutorialRepository
	writes int
}

func (r *tutorialUploadRepo) GetByID(context.Context, int64, bool) (*service.Tutorial, error) {
	return &service.Tutorial{ID: 1}, nil
}
func (r *tutorialUploadRepo) CreateAsset(_ context.Context, a *service.TutorialAsset) error {
	r.writes++
	if r.writes > 1 {
		return errors.New("simulated metadata failure")
	}
	a.ID = 1
	return nil
}
func TestTutorialUploadMetadataFailureDoesNotDeleteSharedFile(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{}
	cfg.Pricing.DataDir = t.TempDir()
	cfg.JWT.Secret = "test-secret"
	repo := &tutorialUploadRepo{}
	h := NewTutorialHandler(service.NewTutorialService(repo), cfg)
	router := gin.New()
	router.POST("/tutorials/:id/assets", func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1})
		h.UploadAsset(c)
	})
	var imageData bytes.Buffer
	require.NoError(t, png.Encode(&imageData, image.NewRGBA(image.Rect(0, 0, 2, 2))))
	upload := func() *httptest.ResponseRecorder {
		var body bytes.Buffer
		w := multipart.NewWriter(&body)
		f, e := w.CreateFormFile("file", "same.png")
		require.NoError(t, e)
		_, e = f.Write(imageData.Bytes())
		require.NoError(t, e)
		require.NoError(t, w.Close())
		req := httptest.NewRequest("POST", "/tutorials/1/assets", &body)
		req.Header.Set("Content-Type", w.FormDataContentType())
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		return res
	}
	require.Equal(t, 200, upload().Code)
	require.Equal(t, 500, upload().Code)
	files, e := os.ReadDir(filepath.Join(cfg.Pricing.DataDir, "tutorial-assets"))
	require.NoError(t, e)
	require.Len(t, files, 1)
	b, e := os.ReadFile(filepath.Join(cfg.Pricing.DataDir, "tutorial-assets", files[0].Name()))
	require.NoError(t, e)
	require.Equal(t, imageData.Bytes(), b)
}
