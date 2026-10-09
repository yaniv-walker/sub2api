package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type tutorialRepoFake struct{}

func (tutorialRepoFake) List(context.Context, TutorialListParams) ([]Tutorial, int64, error) {
	return nil, 0, nil
}
func (tutorialRepoFake) GetByID(context.Context, int64, bool) (*Tutorial, error) {
	return nil, ErrTutorialNotFound
}
func (tutorialRepoFake) GetBySlug(context.Context, string, bool) (*Tutorial, error) {
	return nil, ErrTutorialNotFound
}
func (tutorialRepoFake) Create(context.Context, *Tutorial) error { return nil }
func (tutorialRepoFake) Update(context.Context, *Tutorial) error { return nil }
func (tutorialRepoFake) Delete(context.Context, int64) error     { return nil }
func (tutorialRepoFake) SetStatus(context.Context, int64, string, *time.Time, int64) error {
	return nil
}
func (tutorialRepoFake) ListAssets(context.Context, int64) ([]TutorialAsset, error) { return nil, nil }
func (tutorialRepoFake) GetAssetByKey(context.Context, string) (*TutorialAsset, error) {
	return nil, ErrTutorialNotFound
}
func (tutorialRepoFake) CreateAsset(context.Context, *TutorialAsset) error { return nil }
func (tutorialRepoFake) DeleteAsset(context.Context, int64) error          { return nil }

func TestTutorialServiceValidatesSlugAndUnsafeContent(t *testing.T) {
	s := NewTutorialService(tutorialRepoFake{})
	err := s.Create(context.Background(), &Tutorial{Slug: "Bad Slug", Title: "教程", ContentMarkdown: "# 内容"})
	require.ErrorIs(t, err, ErrTutorialInvalidSlug)
	err = s.Create(context.Background(), &Tutorial{Slug: "valid-slug", Title: "教程", ContentHTML: "<script>alert(1)</script>"})
	require.Error(t, err)
}

func TestTutorialServiceDefaultsCategoryAndStatus(t *testing.T) {
	s := NewTutorialService(tutorialRepoFake{})
	tutorial := &Tutorial{Slug: "valid-slug", Title: "教程", ContentMarkdown: "# 内容"}
	require.NoError(t, s.Create(context.Background(), tutorial))
	require.Equal(t, "getting-started", tutorial.Category)
	require.Equal(t, TutorialStatusDraft, tutorial.Status)
}
