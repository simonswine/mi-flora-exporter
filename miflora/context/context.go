package context

import (
	"context"
	"time"

	"github.com/simonswine/mi-flora-exporter/miflora/model"
)

type contextKey int

const (
	contextScanTimeout contextKey = iota
	contextScanPassive
	contextExpectedSensors
	contextSensorNames
	contextResultChannel
)

func ContextWithScanTimeout(ctx context.Context, t time.Duration) context.Context {
	return context.WithValue(ctx, contextScanTimeout, t)
}

func ScanTimeoutFromContext(ctx context.Context) time.Duration {
	if ctx != nil {
		if v := ctx.Value(contextScanTimeout); v != nil {
			if v, ok := v.(time.Duration); ok {
				return v
			}
		}
	}
	return 5 * time.Second
}

func ContextWithScanPassive(ctx context.Context, v bool) context.Context {
	return context.WithValue(ctx, contextScanPassive, v)
}

func ScanPassiveFromContext(ctx context.Context) bool {
	if ctx != nil {
		if v := ctx.Value(contextScanPassive); v != nil {
			if v, ok := v.(bool); ok {
				return v
			}
		}
	}
	return false
}

func ContextWithExpectedSensors(ctx context.Context, n int64) context.Context {
	return context.WithValue(ctx, contextExpectedSensors, n)
}

func ExpectedSensorsFromContext(ctx context.Context) int64 {
	if ctx != nil {
		if v := ctx.Value(contextExpectedSensors); v != nil {
			if v, ok := v.(int64); ok {
				return v
			}
		}
	}
	return 0
}

func ContextWithSensorNames(ctx context.Context, v []string) context.Context {
	return context.WithValue(ctx, contextSensorNames, v)
}

func SensorsNamesFromContext(ctx context.Context) []string {
	if ctx != nil {
		if v := ctx.Value(contextSensorNames); v != nil {
			if v, ok := v.([]string); ok {
				return v
			}
		}
	}
	return []string{}
}

func ContextWithResultChannel(ctx context.Context, c chan *model.Result) context.Context {
	return context.WithValue(ctx, contextResultChannel, c)
}

func ResultChannelFromContext(ctx context.Context) chan *model.Result {
	if ctx != nil {
		if c := ctx.Value(contextResultChannel); c != nil {
			if c, ok := c.(chan *model.Result); ok {
				return c
			}
		}
	}
	return nil
}
