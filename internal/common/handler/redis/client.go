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

// luaFlashSaleRollback 把秒杀的 Lua 原子 reserve 撤回 —— INCRBY 还库存 + DEL once key。
//
// 关键边界:**TTL 已过期(活动结束)时,stock key 可能已被 Redis 自动清掉**。
// 此时 INCRBY 会**新建一个没有 TTL 的 key**——长期漏水。
// 修法:用 EXISTS 判断,key 不存在时跳过 INCRBY(活动已结束,补偿那个名额没意义)。
// once key 总是 DEL —— 即使活动结束,也让该 customer 可以参与下一场。
//
// 返回值:
//   1 → INCRBY 实际执行了(stock key 存在)
//   0 → 跳过了 INCRBY(stock key 不存在,活动已结束)
var luaFlashSaleRollback = redis.NewScript(`
  local exists = redis.call("EXISTS", KEYS[1])
  if exists == 1 then
    redis.call("INCRBY", KEYS[1], ARGV[1])
  end
  redis.call("DEL", KEYS[2])
  return exists
  `)

// FlashRollbackResult 表示一次 rollback 的实际效果。
type FlashRollbackResult int

const (
	FlashRollbackApplied FlashRollbackResult = 1 // 库存还回 + once 删
	FlashRollbackSkipped FlashRollbackResult = 0 // stock key 已过期,只删 once
)

// rollbackRetryAttempts 是 transient Redis 故障的本地重试次数。
// 超过这个数仍失败 → caller 必须从 metric / 日志察觉,人工对账。
const rollbackRetryAttempts = 3

// FlashSaleRollback 调用 luaFlashSaleRollback 并对 transient Redis 错误做指数退避重试。
// 持续失败时返回最后一次的 error,caller 应当 emit metric / 日志。
//
// 注意:Lua 本身原子;重试只针对**网络层 transient 失败**(超时 / 连接错),
// 同一组 (stockKey, onceKey, quantity) 多次执行**不安全**——会重复 INCRBY。
// 所以重试**仅在 Lua 返回 RedisError 时触发**,语义错误(脚本本身报错)不重试。
func FlashSaleRollback(
	ctx context.Context,
	client *redis.Client,
	stockKey, onceKey string,
	quantity int64,
) (FlashRollbackResult, error) {
	if client == nil {
		return 0, errors.New("redis client is nil")
	}
	var lastErr error
	for attempt := 0; attempt < rollbackRetryAttempts; attempt++ {
		raw, err := luaFlashSaleRollback.Run(ctx, client,
			[]string{stockKey, onceKey}, quantity,
		).Result()
		if err == nil {
			code, _ := raw.(int64)
			return FlashRollbackResult(code), nil
		}
		lastErr = err
		// 指数退避:50ms / 100ms / 200ms。最长总等 350ms,不超过 HTTP / consumer 超时预算。
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		backoff := time.Duration(50*(1<<attempt)) * time.Millisecond
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(backoff):
		}
	}
	return 0, lastErr
}
