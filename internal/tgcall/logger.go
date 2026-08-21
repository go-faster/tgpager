package tgcall

import (
	"context"

	"github.com/gotd/log"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type zapGotdLogger struct {
	lg *zap.Logger
}

func zapToGotdLog(lg *zap.Logger) log.Logger {
	return &zapGotdLogger{lg: lg}
}

func (z *zapGotdLogger) Enabled(_ context.Context, level log.Level) bool {
	return z.lg.Core().Enabled(gotdLevelToZap(level))
}

func (z *zapGotdLogger) Log(_ context.Context, level log.Level, msg string, attrs ...log.Attr) {
	fields := make([]zap.Field, 0, len(attrs))
	for _, a := range attrs {
		fields = append(fields, gotdAttrToZap(a))
	}
	z.lg.Log(gotdLevelToZap(level), msg, fields...)
}

func gotdLevelToZap(level log.Level) zapcore.Level {
	switch {
	case level <= log.LevelDebug:
		return zap.DebugLevel
	case level <= log.LevelInfo:
		return zap.InfoLevel
	case level <= log.LevelWarn:
		return zap.WarnLevel
	default:
		return zap.ErrorLevel
	}
}

func gotdAttrToZap(a log.Attr) zap.Field {
	switch a.Value.Kind() {
	case log.KindAny:
		return zap.Any(a.Key, a.Value.Any())
	case log.KindBool:
		return zap.Bool(a.Key, a.Value.Bool())
	case log.KindDuration:
		return zap.Duration(a.Key, a.Value.Duration())
	case log.KindError:
		return zap.Error(a.Value.Error())
	case log.KindFloat64:
		return zap.Float64(a.Key, a.Value.Float64())
	case log.KindInt64:
		return zap.Int64(a.Key, a.Value.Int64())
	case log.KindString:
		return zap.String(a.Key, a.Value.String())
	case log.KindTime:
		return zap.Time(a.Key, a.Value.Time())
	case log.KindUint64:
		return zap.Uint64(a.Key, a.Value.Uint64())
	default:
		return zap.Any(a.Key, a.Value.Any())
	}
}
