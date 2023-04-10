package p9p

import (
	"context"
	"time"
)

type contextKey string

const (
	versionKey contextKey = "9p.version"
)

func withVersion(ctx context.Context, version string) context.Context {
	return context.WithValue(ctx, versionKey, version)
}

// GetVersion returns the protocol version from the context. If the version is
// not known, an empty string is returned. This is typically set on the
// context passed into function calls in a server implementation.
func GetVersion(ctx context.Context) string {
	v, ok := ctx.Value(versionKey).(string)
	if !ok {
		return ""
	}
	return v
}

// Simple context representing a past-due deadline.
type CancelledCtxt struct{}

func (_ CancelledCtxt) Deadline() (time.Time, bool) {
	return time.Time{}, false
}

func (_ CancelledCtxt) Done() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}

func (_ CancelledCtxt) Err() error {
	return context.Canceled
}

func (_ CancelledCtxt) Value(key any) (val any) {
	return nil
}
