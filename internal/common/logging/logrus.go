package logging

import (
	"context"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ecstasoy/gorder/common/tracing"
	rotatelogs "github.com/lestrrat-go/file-rotatelogs"
	"github.com/rifflock/lfshook"
	"github.com/sirupsen/logrus"
	prefix "github.com/x-cray/logrus-prefixed-formatter"
)

func Init() {
	SetFormatter(logrus.StandardLogger())
	setOutput(logrus.StandardLogger())
	logrus.SetLevel(logrus.DebugLevel)
	logrus.AddHook(&traceHook{})
	setLevel(logrus.StandardLogger())
}

func SetFormatter(logger *logrus.Logger) {
	logger.SetFormatter(&logrus.JSONFormatter{
		TimestampFormat: time.RFC3339Nano,
	})
	if isLocal, _ := strconv.ParseBool(os.Getenv("LOCAL_ENV")); isLocal {
		logger.SetFormatter(&prefix.TextFormatter{
			ForceFormatting: true,
			ForceColors:     true,
			FullTimestamp:   true,
			TimestampFormat: time.RFC3339Nano,
		})
	}
}

func setOutput(logger *logrus.Logger) {
	logger.SetOutput(os.Stdout)

	if strings.ToLower(os.Getenv("LOG_OUTPUT")) != "file" {
		return
	}

	const (
		folder    = "./log/"
		appFile   = "app.log"
		errorFile = "errors.log"
	)
	if err := os.MkdirAll(folder, 0750); err != nil && !os.IsExist(err) {
		panic(err)
	}
	if err := os.MkdirAll(folder, 0750); err != nil && !os.IsExist(err) {
		panic(err)
	}

	rotateInfo, err := rotatelogs.New(
		folder+appFile+".%Y%m%d",
		rotatelogs.WithLinkName(folder+appFile),
		rotatelogs.WithMaxAge(7*24*time.Hour),
		rotatelogs.WithRotationTime(1*time.Hour),
	)
	if err != nil {
		panic(err)
	}
	rotateError, err := rotatelogs.New(
		folder+errorFile+".%Y%m%d",
		rotatelogs.WithLinkName(folder+errorFile),
		rotatelogs.WithMaxAge(7*24*time.Hour),
		rotatelogs.WithRotationTime(1*time.Hour),
	)
	if err != nil {
		panic(err)
	}
	logger.AddHook(lfshook.NewHook(lfshook.WriterMap{
		logrus.DebugLevel: rotateInfo,
		logrus.InfoLevel:  rotateInfo,
		logrus.WarnLevel:  rotateError,
		logrus.ErrorLevel: rotateError,
		logrus.FatalLevel: rotateError,
		logrus.PanicLevel: rotateError,
	}, &logrus.JSONFormatter{
		TimestampFormat: time.RFC3339Nano,
	}))
}

func setLevel(logger *logrus.Logger) {
	if lvl := os.Getenv("LOG_LEVEL"); lvl != "" {
		parsed, err := logrus.ParseLevel(strings.ToUpper(lvl))
		if err != nil {
			logger.SetLevel(logrus.InfoLevel)
			logger.Warnf("invalid LOG_LEVEL=%q, falling back to info", lvl)
			return
		}
		logger.SetLevel(parsed)
		return
	}
	if isLocal, _ := strconv.ParseBool(os.Getenv("LOCAL_ENV")); isLocal {
		logger.SetLevel(logrus.DebugLevel)
		return
	}
	logger.SetLevel(logrus.InfoLevel)
}

func logf(ctx context.Context, level logrus.Level, fields logrus.Fields, format string, args ...any) {
	logrus.WithContext(ctx).WithFields(fields).Logf(level, format, args...)
}

func InfofWithCost(ctx context.Context, fields logrus.Fields, start time.Time, format string, args ...any) {
	fields[Cost] = time.Since(start).Milliseconds()
	Infof(ctx, fields, format, args...)
}

func Infof(ctx context.Context, fields logrus.Fields, format string, args ...any) {
	logrus.WithContext(ctx).WithFields(fields).Infof(format, args...)
}

func Errorf(ctx context.Context, fields logrus.Fields, format string, args ...any) {
	logrus.WithContext(ctx).WithFields(fields).Errorf(format, args...)
}

func Warnf(ctx context.Context, fields logrus.Fields, format string, args ...any) {
	logrus.WithContext(ctx).WithFields(fields).Warnf(format, args...)
}

func Panicf(ctx context.Context, fields logrus.Fields, format string, args ...any) {
	logrus.WithContext(ctx).WithFields(fields).Panicf(format, args...)
}

type traceHook struct{}

func (t traceHook) Levels() []logrus.Level {
	return logrus.AllLevels
}

func (t traceHook) Fire(entry *logrus.Entry) error {
	if entry.Context != nil {
		entry.Data["hook_trace"] = tracing.TraceID(entry.Context)
		entry = entry.WithTime(time.Now())
	}
	return nil
}
