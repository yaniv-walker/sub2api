package model

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestBalanceAlertConfigValidation(t *testing.T) {
	c := BalanceAlertConfig{Enabled: true, Threshold: "0", Recipients: []string{" ops@example.com ", "ops@example.com"}, Locale: "zh"}
	require.NoError(t, c.Validate())
	require.Equal(t, []string{"ops@example.com"}, c.Recipients)
	for _, threshold := range []string{"", "-1", "NaN", "Inf", "1e2", "0.0000001", "100000000000000"} {
		c.Threshold = threshold
		require.Error(t, c.Validate(), threshold)
	}
	c.Threshold = "100.000001"
	require.NoError(t, c.Validate())
	require.True(t, c.IsLow(100.000001))
	require.False(t, c.IsLow(100.000002))
	c.Recipients = []string{"bad"}
	require.Error(t, c.Validate())
}
