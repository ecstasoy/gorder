package main

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/ecstasoy/gorder/common"
	"github.com/ecstasoy/gorder/common/broker"
	client "github.com/ecstasoy/gorder/common/client/order"
	"github.com/ecstasoy/gorder/common/consts"
	"github.com/ecstasoy/gorder/common/convertor"
	"github.com/ecstasoy/gorder/common/genproto/orderpb"
	hErrors "github.com/ecstasoy/gorder/common/handler/errors"
	"github.com/ecstasoy/gorder/common/handler/redis"
	"github.com/ecstasoy/gorder/order/app"
	"github.com/ecstasoy/gorder/order/app/command"
	"github.com/ecstasoy/gorder/order/app/dto"
	"github.com/ecstasoy/gorder/order/app/query"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
	goredis "github.com/redis/go-redis/v9"
)

const idempotencyKeyPrefix = "idempotency:order:"
const idempotencyKeyTTL = 24 * time.Hour

type HTTPServer struct {
	common.BaseResponse
	app app.Application
}

func (H HTTPServer) PostCustomerCustomerIdOrders(c *gin.Context, customerId string) {
	var (
		req  client.CreateOrderRequest
		err  error
		resp dto.CreateOrderResponse
	)
	defer func() {
		H.Response(c, err, resp)
	}()

	if err = c.ShouldBindJSON(&req); err != nil {
		err = hErrors.NewWithError(consts.ErrnoBindRequestError, err)
		return
	}

	if err = H.validate(req); err != nil {
		err = hErrors.NewWithError(consts.ErrnoRequestValidateError, err)
		return
	}

	// 幂等检查
	idempotencyKey := c.GetHeader("Idempotency-Key")
	if idempotencyKey != "" {
		redisKey := idempotencyKeyPrefix + idempotencyKey
		cached, getErr := redis.Get(c.Request.Context(), redis.LocalClient(), redisKey)
		if getErr == nil {
			// 命中缓存，直接返回上次结果
			resp = dto.CreateOrderResponse{
				OrderID:     cached,
				CustomerID:  req.CustomerId,
				RedirectURL: fmt.Sprintf("%s?customerID=%s&orderID=%s", "http://localhost:9090/payment/success", req.CustomerId, cached),
			}
			return
		} else if !errors.Is(getErr, goredis.Nil) {
			// Redis 故障，记录日志但不阻断流程
			c.Set("idempotency_redis_err", getErr)
		}
	}

	items := make([]*orderpb.ItemWithQuantity, 0, len(req.Items))
	for _, item := range req.Items {
		items = append(items, &orderpb.ItemWithQuantity{
			ItemID:   item.Id,
			Quantity: item.Quantity,
		})
	}

	r, err := H.app.Commands.CreateOrder.Handle(c.Request.Context(), command.CreateOrder{
		CustomerID: req.CustomerId,
		Items:      convertor.NewItemWithQuantityConvertor().ProtosToEntities(items),
	})
	if err != nil {
		return
	}

	resp = dto.CreateOrderResponse{
		OrderID:     r.OrderID,
		CustomerID:  req.CustomerId,
		RedirectURL: fmt.Sprintf("%s?customerID=%s&orderID=%s", "http://localhost:9090/payment/success", req.CustomerId, r.OrderID),
	}

	// 存入 Redis，供后续重复请求直接返回
	if idempotencyKey != "" {
		redisKey := idempotencyKeyPrefix + idempotencyKey
		if _, setErr := redis.SetNX(c.Request.Context(), redis.LocalClient(), redisKey, r.OrderID, idempotencyKeyTTL); setErr != nil {
			c.Set("idempotency_set_err", setErr)
		}
	}
}

func (H HTTPServer) GetCustomerCustomerIdOrdersOrderId(c *gin.Context, customerId string, orderId string) {
	var (
		err  error
		resp any
	)
	defer func() {
		H.Response(c, err, resp)
	}()

	o, err := H.app.Queries.GetCustomerOrder.Handle(c.Request.Context(), query.GetCustomerOrder{
		OrderID:    orderId,
		CustomerID: customerId,
	})
	if err != nil {
		return
	}

	resp = client.Order{
		CustomerId:  o.CustomerID,
		Id:          o.ID,
		Items:       convertor.NewItemConvertor().EntitiesToClients(o.Items),
		PaymentLink: o.PaymentLink,
		Status:      client.OrderStatus(o.Status.String()),
	}
}

func (H HTTPServer) validate(req client.CreateOrderRequest) error {
	for _, v := range req.Items {
		if v.Quantity <= 0 {
			return fmt.Errorf("quantity must be positive, got %d from %s", v.Quantity, v.Id)
		}
	}
	return nil
}

// -------------------------------以下为秒杀相关接口和逻辑--------------------------------

const (
	flashStockKeyPrefix  = "flash:stock:"
	flashResultKeyPrefix = "flash:result:"
)

type FlashSaleHTTPServer struct {
	common.BaseResponse
	app       app.Application
	stockGRPC query.StockService
	amqpCh    *amqp.Channel
}

type warmupRequest struct {
	Items      []flashSaleItem `json:"items"`
	TTLSeconds int64           `json:"ttl_seconds"`
}
type flashSaleItem struct {
	ID       string `json:"id"`
	Quantity int32  `json:"quantity"`
}
type flashOrderRequest struct {
	CustomerID string          `json:"customer_id"`
	Items      []flashSaleItem `json:"items"`
}
type flashOrderResponse struct {
	Token string `json:"token"`
}

func (h FlashSaleHTTPServer) PostFlashSaleWarmup(c *gin.Context) {
	var req warmupRequest
	var err error
	defer func() { h.Response(c, err, nil) }()

	if err = c.ShouldBindJSON(&req); err != nil {
		err = hErrors.NewWithError(consts.ErrnoBindRequestError, err)
		return
	}
	if req.TTLSeconds <= 0 {
		req.TTLSeconds = 3600
	}

	items := make([]*orderpb.ItemWithQuantity, 0, len(req.Items))
	for _, item := range req.Items {
		items = append(items, &orderpb.ItemWithQuantity{
			ItemID:   item.ID,
			Quantity: item.Quantity,
		})
	}

	if err = h.stockGRPC.WarmUpFlashStock(c.Request.Context(), items, req.TTLSeconds); err != nil {
		return
	}
}

func (h FlashSaleHTTPServer) PostFlashSaleOrders(c *gin.Context) {
	var req flashOrderRequest
	var err error
	var resp flashOrderResponse
	defer func() { h.Response(c, err, resp) }()

	if err = c.ShouldBindJSON(&req); err != nil {
		err = hErrors.NewWithError(consts.ErrnoBindRequestError, err)
		return
	}

	if req.CustomerID == "" || len(req.Items) == 0 {
		err = hErrors.NewWithMsgf(consts.ErrnoRequestValidateError, "customer_id and items are required")
		return
	}

	ctx := c.Request.Context()
	redisClient := redis.LocalClient()

	type done struct {
		key string
		qty int64
	}
	var decremented []done

	for _, item := range req.Items {
		key := flashStockKeyPrefix + item.ID
		_, decrErr := redis.DecrStock(ctx, redisClient, key, int64(item.Quantity))
		if decrErr != nil {
			for _, d := range decremented {
				_ = redisClient.IncrBy(ctx, key, d.qty)
			}
			if errors.Is(decrErr, redis.ErrStockKeyNotFound) {
				err = hErrors.NewWithMsgf(consts.ErrnoRequestValidateError,
					fmt.Sprintf("flash sale not active for item %s", item.ID))
			} else if errors.Is(decrErr, redis.ErrStockInsufficient) {
				err = hErrors.NewWithMsgf(consts.ErrnoRequestValidateError,
					fmt.Sprintf("item %s is out of stock", item.ID))
			} else {
				err = hErrors.NewWithError(consts.ErrnoUnknownError, decrErr)
			}
			return
		}
		decremented = append(decremented, done{key, int64(item.Quantity)})
	}

	token := uuid.New().String()

	mqItems := make([]broker.FlashSaleItem, 0, len(req.Items))
	for _, item := range req.Items {
		mqItems = append(mqItems, broker.FlashSaleItem{ItemID: item.ID, Quantity: item.Quantity})
	}

	if pubErr := broker.PublishEvent(ctx, broker.PublishEventReq{
		Channel:  h.amqpCh,
		Routing:  broker.Direct,
		Queue:    broker.EventFlashSaleOrder,
		Exchange: "",
		Body:     broker.FlashSaleOrderPayload{Token: token, CustomerID: req.CustomerID, Items: mqItems},
	}); pubErr != nil {
		// MQ 失败，回滚 Redis
		for _, d := range decremented {
			_ = redisClient.IncrBy(ctx, d.key, d.qty)
		}
		err = hErrors.NewWithError(consts.ErrnoUnknownError, pubErr)
		return
	}

	// 写 pending 占位，防止客户端立刻查到 404
	_ = redisClient.Set(ctx, flashResultKeyPrefix+token, `{"status":"pending"}`, 0)
	resp = flashOrderResponse{Token: token}

}

func (h FlashSaleHTTPServer) GetFlashSaleResult(c *gin.Context) {
	token := c.Param("token")
	val, err := redis.Get(c.Request.Context(), redis.LocalClient(), flashResultKeyPrefix+token)
	if err != nil {
		if errors.Is(err, goredis.Nil) {
			val = "pending"
		}
	}
	c.Data(http.StatusOK, "application/json", []byte(val))
}
