package service

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestUpstreamBalanceEmailTemplateLinkAndIsolation(t *testing.T) {
	s := NewNotificationEmailService(newNotificationEmailMemorySettingRepo(), nil)
	for _, locale := range []string{"zh", "en"} {
		template, err := s.GetTemplate(context.Background(), "upstream.balance_low", locale)
		require.NoError(t, err)
		require.Contains(t, template.HTML, `href="{{upstream_url}}"`)
		preview, err := s.PreviewTemplate(context.Background(), NotificationEmailPreviewInput{
			Event: "upstream.balance_low", Locale: locale,
			Variables: map[string]string{"upstream_name": "test <upstream>", "balance": "0.00", "threshold": "100.00", "upstream_url": "https://upstream.example.com"},
		})
		require.NoError(t, err)
		require.Contains(t, preview.HTML, "test &lt;upstream&gt;")
		require.Contains(t, preview.HTML, `href="https://upstream.example.com"`)
	}
	_, err := s.UpdateTemplate(context.Background(), NotificationEmailEventBalanceLow, "zh", "{{upstream_name}}", "<p>{{balance}}</p>")
	require.Error(t, err)
}
