package logging

import (
	"context"
	"os"
	"strconv"
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
	var (
		folder    = "./log/"
		filePath  = "app.log"
		errorPath = "errors.log"
	)
	if err := os.MkdirAll(folder, 0750); err != nil && !os.IsExist(err) {
		panic(err)
	}
	file, err := os.OpenFile(folder+filePath, os.O_CREATE|os.O_RDWR, 0755)
	if err != nil {
		panic(err)
	}
	_, err = os.OpenFile(folder+errorPath, os.O_CREATE|os.O_RDWR, 0755)
	if err != nil {
		panic(err)
	}
	logger.SetOutput(file)

	rotateInfo, err := rotatelogs.New(
		folder+filePath+".%Y%m%d",
		rotatelogs.WithLinkName("app.log"),
		rotatelogs.WithMaxAge(7*24*time.Hour),
		rotatelogs.WithRotationTime(1*time.Hour),
	)
	if err != nil {
		panic(err)
	}
	rotateError, err := rotatelogs.New(
		folder+errorPath+".%Y%m%d",
		rotatelogs.WithLinkName("errors.log"),
		rotatelogs.WithMaxAge(7*24*time.Hour),
		rotatelogs.WithRotationTime(1*time.Hour),
	)
	rotationMap := lfshook.WriterMap{
		logrus.DebugLevel: rotateInfo,
		logrus.InfoLevel:  rotateInfo,
		logrus.WarnLevel:  rotateError,
		logrus.ErrorLevel: rotateError,
		logrus.FatalLevel: rotateError,
		logrus.PanicLevel: rotateError,
	}
	logrus.AddHook(lfshook.NewHook(rotationMap, &logrus.JSONFormatter{
		TimestampFormat: time.RFC3339Nano,
	}))
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
