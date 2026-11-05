package service

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	_ "golang.org/x/image/webp"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// StoreTutorialAsset streams to a private temporary file. Identical uploads reuse
// an immutable content-addressed file; an error never deletes an existing blob.
func StoreTutorialAsset(dir, name string, r io.Reader) (*TutorialAsset, error) {
	ext := strings.ToLower(filepath.Ext(name))
	var limit int64 = 50 << 20
	kind, mime := "file", ""
	switch ext {
	case ".png":
		kind, mime = "image", "image/png"
	case ".jpg", ".jpeg":
		kind, mime = "image", "image/jpeg"
		ext = ".jpg"
	case ".gif":
		kind, mime = "image", "image/gif"
	case ".webp":
		kind, mime = "image", "image/webp"
	case ".mp4":
		kind, mime = "video", "video/mp4"
	case ".webm":
		kind, mime = "video", "video/webm"
	case ".pdf":
		mime = "application/pdf"
	case ".txt":
		mime = "text/plain"
	case ".zip":
		mime = "application/zip"
	default:
		return nil, fmt.Errorf("%w: unsupported asset format", ErrTutorialInvalidInput)
	}
	if kind == "image" {
		limit = 20 << 20
	}
	if kind == "video" {
		limit = 200 << 20
	}
	if err := os.MkdirAll(dir, 0750); err != nil {
		return nil, err
	}
	f, err := os.CreateTemp(dir, ".upload-*")
	if err != nil {
		return nil, err
	}
	tmp := f.Name()
	defer func() { _ = os.Remove(tmp) }()
	defer func() { _ = f.Close() }()
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, hash), io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if n == 0 || n > limit {
		return nil, fmt.Errorf("%w: asset exceeds size limit", ErrTutorialInvalidInput)
	}
	if _, err = f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	head := make([]byte, 512)
	hn, _ := io.ReadFull(f, head)
	head = head[:hn]
	detected := http.DetectContentType(head)
	valid := false
	switch kind {
	case "image":
		_, _ = f.Seek(0, io.SeekStart)
		cfg, format, e := image.DecodeConfig(f)
		valid = e == nil && cfg.Width > 0 && cfg.Height > 0 && int64(cfg.Width)*int64(cfg.Height) <= 100000000 && ((mime == "image/jpeg" && format == "jpeg") || (mime == "image/png" && format == "png") || (mime == "image/gif" && format == "gif") || (mime == "image/webp" && format == "webp"))
	case "video":
		if ext == ".mp4" {
			valid = len(head) >= 12 && string(head[4:8]) == "ftyp"
		}
		if ext == ".webm" {
			valid = len(head) >= 4 && bytes.Equal(head[:4], []byte{0x1a, 0x45, 0xdf, 0xa3}) && bytes.Contains(head, []byte("webm"))
		}
	default:
		switch ext {
		case ".pdf":
			valid = bytes.HasPrefix(head, []byte("%PDF-"))
		case ".zip":
			valid = bytes.HasPrefix(head, []byte{'P', 'K', 3, 4}) || bytes.HasPrefix(head, []byte{'P', 'K', 5, 6})
		case ".txt":
			valid = strings.HasPrefix(detected, "text/plain") && utf8.Valid(head) && !bytes.ContainsRune(head, 0)
		}
	}
	if !valid {
		return nil, fmt.Errorf("%w: file content does not match its format", ErrTutorialInvalidInput)
	}
	digest := hex.EncodeToString(hash.Sum(nil))
	key := digest + ext
	if err = f.Sync(); err != nil {
		return nil, err
	}
	if err = f.Close(); err != nil {
		return nil, err
	}
	// Link is atomic and fails if the destination already exists (including races).
	if err = os.Link(tmp, filepath.Join(dir, key)); err != nil && !os.IsExist(err) {
		return nil, err
	}
	return &TutorialAsset{Kind: kind, StorageKey: key, PublicURL: "/api/v1/tutorial-assets/" + key, OriginalName: filepath.Base(name), MIMEType: mime, SizeBytes: n, SHA256: digest}, nil
}
