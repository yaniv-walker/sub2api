package service

import (
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
	"time"
)

func TestTutorialRenderingPreservesMediaButRemovesExecutableContent(t *testing.T) {
	html, err := RenderTutorialMarkdown("## 配置\n\n<div data-callout=\"cyan\"><span style=\"color:#dc2626\">注意</span></div>\n\n<img src=\"/api/v1/tutorial-assets/abc.png\" data-width=\"50%\" onerror=\"alert(1)\">\n\n<video controls playsinline src=\"/api/v1/tutorial-assets/clip.mp4\"></video>\n\n<script>alert(1)</script><iframe src=\"https://evil.test\"></iframe><a href=\"javascript:alert(1)\">bad</a>")
	require.NoError(t, err)
	require.Contains(t, html, "<h2")
	require.Contains(t, html, `data-width="50%"`)
	require.Contains(t, html, `data-callout="cyan"`)
	require.Contains(t, html, "<video")
	for _, bad := range []string{"onerror", "<script", "<iframe", "javascript:"} {
		require.NotContains(t, html, bad)
	}
}
func TestTutorialMediaSignatureCannotChangeScopeOrPath(t *testing.T) {
	now := time.Unix(1700000000, 0)
	s := NewTutorialMediaSigner("a-test-secret")
	u := s.URL("abc.png", 12, 7, "published", now.Add(time.Hour))
	require.True(t, s.VerifyURL(u, now))
	require.False(t, s.VerifyURL(strings.Replace(u, "abc.png", "other.png", 1), now))
	require.False(t, s.VerifyURL(strings.Replace(u, "published", "admin", 1), now))
	require.False(t, s.VerifyURL(u, now.Add(2*time.Hour)))
}

func TestTutorialCanonicalContentNeverPersistsSignedMediaQuery(t *testing.T) {
	raw := `![image](https://our.test/api/v1/tutorial-assets/file.png?document=1&expires=123&signature=abc)`
	require.Equal(t, `![image](/api/v1/tutorial-assets/file.png)`, CanonicalTutorialContent(raw))
	t1 := &Tutorial{Slug: "signed", Title: "Signed", ContentMarkdown: raw}
	require.NoError(t, prepareTutorial(t1))
	require.NotContains(t, t1.ContentMarkdown, "signature")
	require.NotContains(t, t1.ContentHTML, "expires")
}
