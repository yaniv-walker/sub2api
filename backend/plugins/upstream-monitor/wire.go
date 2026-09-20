//go:build wireinject
// +build wireinject

package upstreammonitor

import (
	"log/slog"

	"github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/plugins/upstream-monitor/handler"
	"github.com/Wei-Shaw/sub2api/plugins/upstream-monitor/repository"
	"github.com/Wei-Shaw/sub2api/plugins/upstream-monitor/service"
	"github.com/google/wire"
	"github.com/redis/go-redis/v9"
)

// InitializePlugin initializes the upstream monitor plugin with all dependencies.
func InitializePlugin(
	cfg *config.Config,
	entClient *ent.Client,
	redisClient *redis.Client,
	logger *slog.Logger,
) (*Plugin, error) {
	wire.Build(ProviderSet)
	return nil, nil
}
