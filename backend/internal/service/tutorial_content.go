package service

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	stdhtml "html"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/renderer/html"
	xhtml "golang.org/x/net/html"
)

var tutorialMarkdown = goldmark.New(goldmark.WithExtensions(extension.GFM), goldmark.WithRendererOptions(html.WithUnsafe()))
var tutorialAssetWithQuery = regexp.MustCompile(`(?:https?://[^/\s"'<>]+)?/api/v1/tutorial-assets/[a-zA-Z0-9_.-]+\?[^\s"'<>\)]+`)

// CanonicalTutorialContent strips ephemeral capability query strings before storage.
func CanonicalTutorialContent(content string) string {
	return tutorialAssetWithQuery.ReplaceAllStringFunc(content, func(raw string) string {
		start := strings.Index(raw, "/api/v1/tutorial-assets/")
		end := start + strings.Index(raw[start:], "?")
		return raw[start:end]
	})
}

var tutorialAssetURL = regexp.MustCompile(`/api/v1/tutorial-assets/([a-zA-Z0-9_.-]+)`)

// RenderTutorialMarkdown is the only authority for stored tutorial HTML.
// Raw HTML is supported only through the explicit allowlist below.
func RenderTutorialMarkdown(markdown string) (string, error) {
	var out bytes.Buffer
	if err := tutorialMarkdown.Convert([]byte(markdown), &out); err != nil {
		return "", err
	}
	p := bluemonday.UGCPolicy()
	p.AllowElements("video", "source", "mark", "div", "span")
	p.AllowAttrs("controls", "preload", "playsinline", "poster", "src").OnElements("video")
	p.AllowAttrs("src", "type").OnElements("source")
	p.AllowAttrs("data-width").Matching(regexp.MustCompile(`^(25|50|75|100)%$`)).OnElements("img")
	p.AllowAttrs("data-callout").Matching(regexp.MustCompile(`^(cyan|violet|green|rose)$`)).OnElements("div")
	p.AllowStyles("color").Matching(regexp.MustCompile(`^(#[0-9a-fA-F]{3,6}|rgb\([0-9 ,]+\))$`)).OnElements("span")
	p.AllowStyles("background-color").Matching(regexp.MustCompile(`^(#[0-9a-fA-F]{3,6}|rgb\([0-9 ,]+\))$`)).OnElements("mark")
	safe := p.Sanitize(out.String())
	root, err := xhtml.Parse(strings.NewReader(safe))
	if err != nil {
		return "", err
	}
	var walk func(*xhtml.Node)
	walk = func(n *xhtml.Node) {
		if n.Type == xhtml.ElementNode {
			attrs := n.Attr[:0]
			for _, a := range n.Attr {
				if (a.Key == "src" || a.Key == "poster") && !validTutorialMediaURL(a.Val) {
					continue
				}
				if a.Key == "style" {
					parts := strings.SplitN(a.Val, ":", 2)
					if len(parts) != 2 {
						continue
					}
					value := tutorialColor(parts[1])
					if value == "" {
						continue
					}
					if n.Data == "span" && strings.TrimSpace(parts[0]) == "color" {
						a.Val = "color: " + value
					} else if n.Data == "mark" && strings.TrimSpace(parts[0]) == "background-color" {
						a.Val = "background-color: " + value
					} else {
						continue
					}
				}
				attrs = append(attrs, a)
			}
			n.Attr = attrs
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	var result bytes.Buffer
	var body func(*xhtml.Node) *xhtml.Node
	body = func(n *xhtml.Node) *xhtml.Node {
		if n.Type == xhtml.ElementNode && n.Data == "body" {
			return n
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if b := body(c); b != nil {
				return b
			}
		}
		return nil
	}
	if b := body(root); b != nil {
		for c := b.FirstChild; c != nil; c = c.NextSibling {
			if err := xhtml.Render(&result, c); err != nil {
				return "", err
			}
		}
	}
	return result.String(), nil
}
func validTutorialMediaURL(s string) bool {
	u, err := url.Parse(strings.TrimSpace(s))
	if err != nil || u.User != nil {
		return false
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil && (ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified()) {
		return false
	}
	if strings.EqualFold(u.Hostname(), "localhost") {
		return false
	}
	return u.Scheme == "https" && u.Host != "" || u.Scheme == "" && u.Host == "" && strings.HasPrefix(u.Path, "/api/v1/tutorial-assets/") && !strings.Contains(u.Path, "..")
}

// TutorialMediaSigner issues short-lived scoped capabilities, never login tokens.
type TutorialMediaSigner struct{ key []byte }

func NewTutorialMediaSigner(secret string) *TutorialMediaSigner {
	sum := sha256.Sum256([]byte("tutorial-media-v1:" + secret))
	return &TutorialMediaSigner{key: sum[:]}
}
func (s *TutorialMediaSigner) mac(key string, id, version int64, scope string, exp int64) string {
	h := hmac.New(sha256.New, s.key)
	fmt.Fprintf(h, "%s\n%d\n%d\n%s\n%d", key, id, version, scope, exp)
	return hex.EncodeToString(h.Sum(nil))
}
func (s *TutorialMediaSigner) URL(key string, id, version int64, scope string, expiry time.Time) string {
	q := url.Values{"document": {strconv.FormatInt(id, 10)}, "version": {strconv.FormatInt(version, 10)}, "scope": {scope}, "expires": {strconv.FormatInt(expiry.Unix(), 10)}}
	q.Set("signature", s.mac(key, id, version, scope, expiry.Unix()))
	return "/api/v1/tutorial-assets/" + url.PathEscape(key) + "?" + q.Encode()
}
func (s *TutorialMediaSigner) VerifyURL(raw string, now time.Time) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	q := u.Query()
	id, e1 := strconv.ParseInt(q.Get("document"), 10, 64)
	v, e2 := strconv.ParseInt(q.Get("version"), 10, 64)
	exp, e3 := strconv.ParseInt(q.Get("expires"), 10, 64)
	scope := q.Get("scope")
	if e1 != nil || e2 != nil || e3 != nil || id <= 0 || exp <= now.Unix() || exp > now.Add(3*time.Hour).Unix() || (scope != "admin" && scope != "published" && scope != "public" && scope != "support") {
		return false
	}
	key := strings.TrimPrefix(u.Path, "/api/v1/tutorial-assets/")
	if key == "" || strings.ContainsAny(key, "/\\") {
		return false
	}
	got, err := hex.DecodeString(q.Get("signature"))
	if err != nil {
		return false
	}
	want, _ := hex.DecodeString(s.mac(key, id, v, scope, exp))
	return hmac.Equal(got, want)
}
func (s *TutorialMediaSigner) SignHTML(t *Tutorial, scope string) string {
	return tutorialAssetURL.ReplaceAllStringFunc(t.ContentHTML, func(raw string) string {
		m := tutorialAssetURL.FindStringSubmatch(raw)
		return stdhtml.EscapeString(s.URL(m[1], t.ID, t.Version, scope, time.Now().Add(2*time.Hour)))
	})
}
func TutorialAssetKeys(content string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range tutorialAssetURL.FindAllStringSubmatch(content, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	return out
}

func tutorialColor(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(raw, ";")))
	for _, c := range []string{"#475569", "#dc2626", "#c2410c", "#a16207", "#15803d", "#2563eb", "#7c3aed", "#fef08a", "#fed7aa", "#bbf7d0", "#bfdbfe", "#fbcfe8", "#e2e8f0"} {
		if raw == c {
			return c
		}
		r, _ := strconv.ParseUint(c[1:3], 16, 8)
		g, _ := strconv.ParseUint(c[3:5], 16, 8)
		b, _ := strconv.ParseUint(c[5:7], 16, 8)
		if strings.ReplaceAll(raw, " ", "") == fmt.Sprintf("rgb(%d,%d,%d)", r, g, b) {
			return c
		}
	}
	return ""
}
