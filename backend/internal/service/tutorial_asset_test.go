package service

import (
	"bytes"
	"github.com/stretchr/testify/require"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestTutorialAssetRepeatedUploadKeepsExistingBytes(t *testing.T) {
	var b bytes.Buffer
	require.NoError(t, png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 2, 2))))
	dir := t.TempDir()
	a, e := StoreTutorialAsset(dir, "one.png", bytes.NewReader(b.Bytes()))
	require.NoError(t, e)
	a2, e := StoreTutorialAsset(dir, "two.png", bytes.NewReader(b.Bytes()))
	require.NoError(t, e)
	require.Equal(t, a.StorageKey, a2.StorageKey)
	got, e := os.ReadFile(filepath.Join(dir, a.StorageKey))
	require.NoError(t, e)
	require.Equal(t, b.Bytes(), got)
}
func TestTutorialAssetRejectsPretendImage(t *testing.T) {
	_, e := StoreTutorialAsset(t.TempDir(), "fake.png", bytes.NewBufferString("<svg onload='alert(1)'></svg>"))
	require.Error(t, e)
}
