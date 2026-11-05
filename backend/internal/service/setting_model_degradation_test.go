package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type modelDegradationSettingRepo struct {
	SettingRepository
	value string
	err   error
}

func (r modelDegradationSettingRepo) GetValue(context.Context, string) (string, error) {
	return r.value, r.err
}

func TestModelDegradationEnabledDefaultsAndStorageFailure(t *testing.T) {
	for _, tc := range []struct {
		name, value string
		err         error
		want        bool
	}{
		{"existing deployment", "", ErrSettingNotFound, true},
		{"enabled", "true", nil, true},
		{"disabled", "false", nil, false},
		{"storage unavailable", "", errors.New("unavailable"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := NewSettingService(modelDegradationSettingRepo{value: tc.value, err: tc.err}, nil)
			on, err := svc.ModelDegradationEnabled(t.Context())
			require.Equal(t, tc.want, on)
			if tc.err != nil && !errors.Is(tc.err, ErrSettingNotFound) {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
