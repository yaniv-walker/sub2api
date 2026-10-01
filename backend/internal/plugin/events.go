package plugin

import "time"

// RequestEvent is the stable payload shared by host request handling and plugins.
type RequestEvent struct {
	AccountID    int64
	UpstreamType string
	Model        string
	RequestID    string
	ErrorType    string
	ErrorMessage string
	HTTPStatus   *int
	OccurredAt   time.Time
}

// AccountDeletedEvent identifies an account whose plugin-owned data must be removed.
type AccountDeletedEvent struct {
	AccountID    int64
	UpstreamType string
}
