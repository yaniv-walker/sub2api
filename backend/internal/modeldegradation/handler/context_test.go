//go:build unit

package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
)

// 这组测试锁住「从上下文读当前用户」这条链路。
//
// 背景：handler 曾经自己 c.Get("user_id") 去取用户 ID，但 jwt 中间件
// 往上下文放的是 ContextKeyUser → AuthSubject 结构体，根本没有 "user_id"
// 这个键。于是所有需要身份的接口（发起检测、我的检测、取消）在带着
// 有效 token 时也一律返回 401，而编译、单测、lint 全都不会报错。
//
// 这个 bug 只有在真实服务上打接口才会暴露，所以必须用中间件的真实
// 写入方式构造上下文来测。

// newCtxWithSubject 构造一个上下文，用中间件真实的写入方式放身份。
func newCtxWithSubject(userID int64) *gin.Context {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	if userID > 0 {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: userID})
	}
	return c
}

func TestUserIDFromContextReadsMiddlewareSubject(t *testing.T) {
	if got := UserIDFromContext(newCtxWithSubject(42)); got != 42 {
		t.Fatalf("UserIDFromContext = %d, want 42（没读到中间件写入的身份）", got)
	}
}

func TestUserIDFromContextMissingSubject(t *testing.T) {
	if got := UserIDFromContext(newCtxWithSubject(0)); got != 0 {
		t.Fatalf("无身份时应返回 0，实际 %d", got)
	}
}

// TestUserIDFromContextIgnoresLegacyKey 说明旧的 "user_id" 键不再生效。
//
// 保留这个测试是为了让「重新加回 c.Get("user_id")」的改动直接变红。
func TestUserIDFromContextIgnoresLegacyKey(t *testing.T) {
	c := newCtxWithSubject(0)
	c.Set("user_id", int64(99))
	if got := UserIDFromContext(c); got != 0 {
		t.Fatalf("不该从旧键取值，实际 %d", got)
	}
}

// TestUserIDFromContextWrongTypeIsZero 保证类型不对时安全退化为 0。
func TestUserIDFromContextWrongTypeIsZero(t *testing.T) {
	c := newCtxWithSubject(0)
	c.Set(string(middleware.ContextKeyUser), "not-a-subject")
	if got := UserIDFromContext(c); got != 0 {
		t.Fatalf("类型不符时应返回 0，实际 %d", got)
	}
}
