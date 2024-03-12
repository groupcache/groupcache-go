package data

import (
	"context"
	"time"
)

type Group interface {
	Set(context.Context, string, []byte, time.Time, bool) error
	Get(context.Context, string, Sink) error
	Remove(context.Context, string) error
	UsedBytes() (int64, int64)
	Name() string
}
