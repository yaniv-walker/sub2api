// Package module 是「模型降智检测」的 Wire 装配层。
//
// 为什么单独一个包：根包 `modeldegradation` 被 handler / service / repository
// 广泛引用（它们都需要领域类型），如果 ProviderSet 也放在根包，就会形成
// `modeldegradation → handler → modeldegradation` 的导入环。把装配集中在这个
// 只被 cmd/server 引用的叶子包里，依赖方向保持单向：
//
//	module → handler / service / repository → modeldegradation
//
// 约定（与 internal/iqbenchmark/module、internal/securityaudit 一致）：
// cmd/server/wire.go 只需 `mdmodule.ProviderSet` 一行接入。
package module

import (
	"github.com/google/wire"

	"github.com/Wei-Shaw/sub2api/internal/modeldegradation"
	"github.com/Wei-Shaw/sub2api/internal/modeldegradation/handler"
	"github.com/Wei-Shaw/sub2api/internal/modeldegradation/repository"
)

// ProviderSet 汇总本模块的全部构造器。
//
// 入参全部是 Wire 图中**已存在**的类型（*sql.DB、*slog.Logger），
// 因此不需要在上游新增任何 provider。
var ProviderSet = wire.NewSet(
	// 数据层
	repository.New,
	wire.Bind(new(modeldegradation.Store), new(*repository.Repository)),

	// 服务层
	modeldegradation.NewService,

	// HTTP 层
	handler.New,
)
