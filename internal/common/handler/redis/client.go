package redis

import (
	"context"
	"encoding/json"
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

// ---- Flash sale product metadata cache (Name + PriceID snapshot) ----

type FlashMeta struct {
	Name    string `json:"name"`
	PriceID string `json:"price_id"`
}

func SetFlashMeta(ctx context.Context, client *redis.Client, key string, meta FlashMeta, ttl time.Duration) (err error) {
	now := time.Now()
	defer func() {
		l := logrus.WithContext(ctx).WithFields(logrus.Fields{
			"start":       now,
			"key":         key,
			logging.Error: err,
			logging.Cost:  time.Since(now).Milliseconds(),
		})
		if err != nil {
			l.Warn("_redis_set_flash_meta_failed")
		} else {
			l.Info("_redis_set_flash_meta_success")
		}
	}()

	if client == nil {
		return errors.New("redis client is nil")
	}
	b, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	return client.Set(ctx, key, b, ttl).Err()
}

func GetFlashMeta(ctx context.Context, client *redis.Client, key string) (meta *FlashMeta, err error) {
	if client == nil {
		return nil, errors.New("redis client is nil")
	}
	val, err := client.Get(ctx, key).Result()
	if err != nil {
		return nil, err
	}
	m := &FlashMeta{}
	if err = json.Unmarshal([]byte(val), m); err != nil {
		return nil, err
	}

	return m, nil
}

// ---- Flash sale atomic reserve (Lua-merged entry) ----

// FlashReserveResult 秒杀 Lua 的返回码
type FlashReserveResult int

const (
	FlashReserveOK           FlashReserveResult = 0
	FlashReserveNotActive    FlashReserveResult = -1
	FlashReserveInsufficient FlashReserveResult = -2
	FlashReserveDuplicate    FlashReserveResult = -3
)

var luaFlashSaleReserve = redis.NewScript(`
  local stock = redis.call("GET", KEYS[1])
  if stock == false then
      return {-1, 0}
  end

  local ttl = redis.call("TTL", KEYS[1])
  if ttl <= 0 then
      ttl = tonumber(ARGV[3])
  end

  if redis.call("EXISTS", KEYS[2]) == 1 then
      return {-3, tonumber(stock)}
  end

  local qty = tonumber(stock)
  local want = tonumber(ARGV[1])
  if qty < want then
      return {-2, qty}
  end

  redis.call("DECRBY", KEYS[1], want)
  redis.call("SET", KEYS[2], ARGV[2], "EX", ttl)
  return {0, qty - want}
  `)

func FlashSaleReserve(
	ctx context.Context,
	client *redis.Client,
	stockKey, onceKey, token string,
	quantity int64,
	fallbackTTLSeconds int64,
) (FlashReserveResult, int64, error) {
	if client == nil {
		return 0, 0, errors.New("redis client is nil")
	}
	raw, err := luaFlashSaleReserve.Run(ctx, client,
		[]string{stockKey, onceKey},
		quantity, token, fallbackTTLSeconds,
	).Result()
	if err != nil {
		return 0, 0, err
	}
	arr, ok := raw.([]interface{})
	if !ok || len(arr) != 2 {
		return 0, 0, errors.New("unexpected lua flash reserve response shape")
	}
	codeInt, _ := arr[0].(int64)
	remainInt, _ := arr[1].(int64)
	return FlashReserveResult(codeInt), remainInt, nil
}

var luaFlashSaleRollback = redis.NewScript(`
  redis.call("INCRBY", KEYS[1], ARGV[1])
  redis.call("DEL", KEYS[2])
  return 1
  `)

func FlashSaleRollback(
	ctx context.Context,
	client *redis.Client,
	stockKey, onceKey string,
	quantity int64,
) error {
	if client == nil {
		return errors.New("redis client is nil")
	}
	_, err := luaFlashSaleRollback.Run(ctx, client,
		[]string{stockKey, onceKey}, quantity,
	).Result()
	return err
}
