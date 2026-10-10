package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/requestobs"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// validGatewayJSON observes syntax validation only, not later field extraction.
func validGatewayJSON(c *gin.Context, body []byte) bool {
	defer requestobs.StartStage(c.Request.Context(), requestobs.StageJSONValidation)()
	return gjson.ValidBytes(body)
}
