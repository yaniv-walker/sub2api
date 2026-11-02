package handler

import "github.com/gin-gonic/gin"

// HandlerFunc 是 gin.HandlerFunc 的别名。
//
// 单独声明别名是为了让 RateLimiter 接口出现在本包时不必 import gin 两次，
// 同时保持与 gin 中间件类型完全兼容（可直接 Use）。
type HandlerFunc = gin.HandlerFunc
