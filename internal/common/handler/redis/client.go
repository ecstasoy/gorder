package redis

import (
	"context"
	"errors"
	"time"

	"github.com/ecstasoy/gorder/common/logging"
	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
)

const (
	ErrStockKeyNotFound  = stockErr("flash sale stock key not found")
	ErrStockInsufficient = stockErr("insufficient flash sale stock")
)

type stockErr string

func (e stockErr) Error() string { return string(e) }

var luaDecrStock = redis.NewScript(`
  local current = redis.call("GET", KEYS[1])
  if current == false then
      return -1
  end
  local qty = tonumber(current)
  local want = tonumber(ARGV[1])
  if qty < want then
      return -2
  end
  redis.call("DECRBY", KEYS[1], want)
  return qty - want
  `)

func DecrStock(ctx context.Context, client *redis.Client, key string, quantity int64) (remaining int64, err error) {
	now := time.Now()
	defer func() {
		l := logrus.WithContext(ctx).WithFields(logrus.Fields{
			"start":       now,
			"key":         key,
			"quantity":    quantity,
			logging.Error: err,
			logging.Cost:  time.Since(now).Milliseconds(),
		})
		if err != nil {
			l.Warn("_redis_decr_stock_failed")
		} else {
			l.Info("_redis_decr_stock_success")
		}
	}()

	if client == nil {
		return 0, errors.New("redis client is nil")
	}

	result, err := luaDecrStock.Run(ctx, client, []string{key}, quantity).Int64()
	if err != nil {
		return 0, err
	}
	switch result {
	case -1:
		return 0, ErrStockKeyNotFound
	case -2:
		return 0, ErrStockInsufficient
	default:
		return result, nil
	}
}

func SetFlashStock(ctx context.Context, client *redis.Client, key string, quantity int32, ttl time.Duration) (err error) {
	now := time.Now()
	defer func() {
		l := logrus.WithContext(ctx).WithFields(logrus.Fields{
			"start":       now,
			"key":         key,
			"quantity":    quantity,
			logging.Error: err,
			logging.Cost:  time.Since(now).Milliseconds(),
		})
		if err != nil {
			l.Warn("_redis_set_flash_stock_failed")
		} else {
			l.Info("_redis_set_flash_stock_success")
		}
	}()

	if client == nil {
		return errors.New("redis client is nil")
	}

	return client.Set(ctx, key, quantity, ttl).Err()
}

// SetNX 仅在 key 不存在时设置，返回 true 表示设置成功（首次），false 表示已存在。
func SetNX(ctx context.Context, client *redis.Client, key, value string, ttl time.Duration) (ok bool, err error) {
	now := time.Now()
	defer func() {
		l := logrus.WithContext(ctx).WithFields(logrus.Fields{
			"start":       now,
			"key":         key,
			logging.Error: err,
			logging.Cost:  time.Since(now).Milliseconds(),
		})
		if err != nil {
			l.Warn("_redis_setnx_failed")
		} else {
			l.Info("_redis_setnx_success")
		}
	}()
	if client == nil {
		return false, errors.New("redis client is nil")
	}
	ok, err = client.SetNX(ctx, key, value, ttl).Result()
	return ok, err
}

func Get(ctx context.Context, client *redis.Client, key string) (val string, err error) {
	now := time.Now()
	defer func() {
		logrus.WithContext(ctx).WithFields(logrus.Fields{
			"start":       now,
			"key":         key,
			logging.Error: err,
			logging.Cost:  time.Since(now).Milliseconds(),
		}).Info("_redis_get")
	}()
	if client == nil {
		return "", errors.New("redis client is nil")
	}
	return client.Get(ctx, key).Result()
}

func Del(ctx context.Context, client *redis.Client, key string) (err error) {
	now := time.Now()
	defer func() {
		l := logrus.WithContext(ctx).WithFields(logrus.Fields{
			"start": now,
			"key":   key,
			"err":   err,
			"cost":  time.Since(now).Milliseconds(),
		})
		if err != nil {
			l.Error("_redis_del_failed")
		} else {
			l.Info("_redis_del_success")
		}
	}()
	if client == nil {
		return errors.New("redis client is nil")
	}
	_, err = client.Del(ctx, key).Result()
	return err
}
