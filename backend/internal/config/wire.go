package config

import (
	"log/slog"
	"os"

	"github.com/google/wire"
)

// ProviderSet 提供配置层的依赖
var ProviderSet = wire.NewSet(
	ProvideConfig,
	ProvideLogger,
)

// ProvideConfig 提供应用配置
func ProvideConfig() (*Config, error) {
	return LoadForBootstrap()
}

// ProvideLogger 提供日志记录器
func ProvideLogger(cfg *Config) *slog.Logger {
	var level slog.Level
	switch cfg.Server.Mode {
	case "debug":
		level = slog.LevelDebug
	case "release":
		level = slog.LevelInfo
	default:
		level = slog.LevelInfo
	}

	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: level,
	})

	return slog.New(handler)
}
