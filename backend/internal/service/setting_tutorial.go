package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// TutorialDocument is the isolated, administrator-editable tutorial payload.
type TutorialDocument struct {
	Title   string `json:"title"`
	Content string `json:"content"`
}

const defaultTutorialTitle = "教程与售后"

const defaultTutorialContent = `# 欢迎使用

这里是教程与售后中心。管理员可以在本页编辑 Markdown 内容，支持：

- 图片：![说明](https://example.com/image.png)
- 视频：使用 HTML video 标签嵌入 mp4
- 视频平台：使用受信任来源的 iframe 嵌入

## 快速开始

1. 在 **API 密钥** 页面创建密钥。
2. 按站点提供的接口地址配置客户端。
3. 遇到问题时，请先查看公告、渠道状态和本页常见问题。

## 售后支持

请在这里补充客服入口、服务时间、工单格式和退款/续费规则。`

func defaultTutorialDocumentJSON() string {
	b, _ := json.Marshal(TutorialDocument{Title: defaultTutorialTitle, Content: defaultTutorialContent})
	return string(b)
}

func (s *SettingService) TutorialEnabled(ctx context.Context) bool {
	if s == nil || s.settingRepo == nil {
		return false
	}
	raw, err := s.settingRepo.GetValue(ctx, SettingKeyTutorialEnabled)
	return err == nil && strings.EqualFold(strings.TrimSpace(raw), "true")
}

func (s *SettingService) GetTutorialDocument(ctx context.Context) (*TutorialDocument, error) {
	raw, err := s.settingRepo.GetValue(ctx, SettingKeyTutorialDocument)
	if err != nil {
		if errors.Is(err, ErrSettingNotFound) {
			return &TutorialDocument{Title: defaultTutorialTitle, Content: defaultTutorialContent}, nil
		}
		return nil, fmt.Errorf("read tutorial document: %w", err)
	}
	if strings.TrimSpace(raw) == "" {
		return &TutorialDocument{Title: defaultTutorialTitle, Content: defaultTutorialContent}, nil
	}
	var doc TutorialDocument
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return nil, fmt.Errorf("decode tutorial document: %w", err)
	}
	if strings.TrimSpace(doc.Title) == "" {
		doc.Title = defaultTutorialTitle
	}
	return &doc, nil
}

func (s *SettingService) UpdateTutorialDocument(ctx context.Context, doc TutorialDocument) error {
	doc.Title = strings.TrimSpace(doc.Title)
	if doc.Title == "" {
		doc.Title = defaultTutorialTitle
	}
	if len([]rune(doc.Title)) > 120 {
		return fmt.Errorf("tutorial title is too long")
	}
	if len([]byte(doc.Content)) > 1024*1024 {
		return fmt.Errorf("tutorial content is too large")
	}
	b, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("encode tutorial document: %w", err)
	}
	return s.settingRepo.Set(ctx, SettingKeyTutorialDocument, string(b))
}

// TutorialSupport contains only administrator-controlled public contact details.
type TutorialSupport struct {
	Title         string `json:"title"`
	Description   string `json:"description"`
	Wechat        string `json:"wechat"`
	QQ            string `json:"qq"`
	QQGroup       string `json:"qq_group"`
	WechatQRURL   string `json:"wechat_qr_url"`
	QQQRURL       string `json:"qq_qr_url"`
	Email         string `json:"email"`
	TicketURL     string `json:"ticket_url"`
	ServiceHours  string `json:"service_hours"`
	PublicEnabled bool   `json:"public_enabled"`
}

const SettingKeyTutorialSupport = "tutorial_support_v2"

// Production repositories atomically validate referenced assets and update the setting.
type tutorialSupportSettingWriter interface {
	SetTutorialSupport(context.Context, string, []string) error
}

func (s *SettingService) GetTutorialSupport(ctx context.Context) (*TutorialSupport, error) {
	raw, err := s.settingRepo.GetValue(ctx, SettingKeyTutorialSupport)
	if errors.Is(err, ErrSettingNotFound) {
		return &TutorialSupport{Title: "联系售后"}, nil
	}
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(raw) == "" {
		return &TutorialSupport{Title: "联系售后"}, nil
	}
	var settings TutorialSupport
	if err = json.Unmarshal([]byte(raw), &settings); err != nil {
		return nil, err
	}
	return &settings, nil
}
func (s *SettingService) UpdateTutorialSupport(ctx context.Context, t TutorialSupport) error {
	t.WechatQRURL = CanonicalTutorialContent(t.WechatQRURL)
	t.QQQRURL = CanonicalTutorialContent(t.QQQRURL)
	if len([]rune(t.Title)) > 160 || len([]rune(t.Description)) > 1000 || len([]rune(t.ServiceHours)) > 300 || len([]rune(t.Wechat)) > 160 || len([]rune(t.QQ)) > 160 || len([]rune(t.QQGroup)) > 160 || len(t.Email) > 320 {
		return ErrTutorialInvalidInput
	}
	for _, raw := range []string{t.WechatQRURL, t.QQQRURL, t.TicketURL} {
		if raw != "" && !validTutorialLink(raw) {
			return fmt.Errorf("%w: support links require HTTPS or a tutorial asset", ErrTutorialInvalidInput)
		}
	}
	b, err := json.Marshal(t)
	if err != nil {
		return err
	}
	if writer, ok := s.settingRepo.(tutorialSupportSettingWriter); ok {
		return writer.SetTutorialSupport(ctx, string(b), TutorialAssetKeys(t.WechatQRURL+"\n"+t.QQQRURL))
	}
	return s.settingRepo.Set(ctx, SettingKeyTutorialSupport, string(b))
}
