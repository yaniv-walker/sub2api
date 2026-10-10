package service

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/pkg/requestobs"
)

// ParseGatewayRequestWithContext is an observation decorator. The original
// parser retains ownership of validation and returns its result unchanged.
func ParseGatewayRequestWithContext(ctx context.Context, body *RequestBodyRef, protocol string) (*ParsedRequest, error) {
	defer requestobs.StartStage(ctx, requestobs.StageJSONParse)()
	return ParseGatewayRequest(body, protocol)
}
