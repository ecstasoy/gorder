package main

import (
	"encoding/json"
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
	"github.com/sirupsen/logrus"
)

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
	flashStockKeyPrefix    = "flash:stock:"
	flashResultKeyPrefix   = "flash:result:"
	flashOnceKeyPrefix     = "flash:once:"
	flashOnceFallbackTTL   = 24 * time.Hour
	flashResultPendingTTL  = 20 * time.Minute
	flashResultContentType = "application/json"
)

type flashResultPayload struct {
	Status  string `json:"status"`
	OrderID string `json:"order_id,omitempty"`
	Message string `json:"message,omitempty"`
}

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
	req.Items = packFlashSaleItems(req.Items)
	for _, item := range req.Items {
		if item.Quantity <= 0 {
			err = hErrors.NewWithMsgf(consts.ErrnoRequestValidateError,
				"quantity must be positive, got %d from %s", item.Quantity, item.ID)
			return
		}
	}

	ctx := c.Request.Context()
	redisClient := redis.LocalClient()
	token := uuid.New().String()
	fallbackTTL := int64(flashOnceFallbackTTL / time.Second)

	// 追踪已预留的 (stockKey, onceKey, qty),供任何后续步骤失败时回滚
	type reservedEntry struct {
		stockKey string
		onceKey  string
		qty      int64
	}
	var reservedList []reservedEntry

	rollbackAll := func() {
		for _, r := range reservedList {
			if rbErr := redis.FlashSaleRollback(ctx, redisClient, r.stockKey, r.onceKey, r.qty); rbErr != nil {
				logrus.WithContext(ctx).Warnf("flash rollback failed for %s: %v", r.stockKey, rbErr)
			}
		}
	}

	// Step 1: 每个 item 用一条 Lua 原子完成 探活 + 一人一单 + 扣减 + 占位
	for _, item := range req.Items {
		stockKey := flashStockKeyPrefix + item.ID
		onceKey := flashOnceOrderKey(req.CustomerID, item.ID)

		code, _, luaErr := redis.FlashSaleReserve(
			ctx, redisClient,
			stockKey, onceKey, token,
			int64(item.Quantity),
			fallbackTTL,
		)
		if luaErr != nil {
			rollbackAll()
			err = hErrors.NewWithError(consts.ErrnoUnknownError, luaErr)
			return
		}
		switch code {
		case redis.FlashReserveOK:
			reservedList = append(reservedList, reservedEntry{
				stockKey: stockKey, onceKey: onceKey, qty: int64(item.Quantity),
			})
		case redis.FlashReserveNotActive:
			rollbackAll()
			err = hErrors.NewWithMsgf(consts.ErrnoRequestValidateError,
				"flash sale not active for item %s", item.ID)
			return
		case redis.FlashReserveDuplicate:
			rollbackAll()
			err = hErrors.NewWithMsgf(consts.ErrnoRequestValidateError,
				"customer %s can only place one flash-sale order for item %s", req.CustomerID, item.ID)
			return
		case redis.FlashReserveInsufficient:
			rollbackAll()
			err = hErrors.NewWithMsgf(consts.ErrnoRequestValidateError,
				"item %s is out of stock", item.ID)
			return
		default:
			rollbackAll()
			err = hErrors.NewWithMsgf(consts.ErrnoUnknownError, "unexpected flash reserve code %d", code)
			return
		}
	}

	// Step 2: Publish
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
		rollbackAll()
		err = hErrors.NewWithError(consts.ErrnoUnknownError, pubErr)
		return
	}

	// Step 3: 写 pending 占位 (TTL 20 分钟)
	_ = redisClient.Set(ctx, flashResultKeyPrefix+token,
		mustMarshalFlashResult(flashResultPayload{Status: "pending"}),
		flashResultPendingTTL)

	resp = flashOrderResponse{Token: token}
}

func (h FlashSaleHTTPServer) GetFlashSaleResult(c *gin.Context) {
	token := c.Param("token")
	val, err := redis.Get(c.Request.Context(), redis.LocalClient(), flashResultKeyPrefix+token)
	if err != nil {
		if errors.Is(err, goredis.Nil) {
			val = mustMarshalFlashResult(flashResultPayload{
				Status:  "not_found",
				Message: "token not found or result expired",
			})
		} else {
			val = mustMarshalFlashResult(flashResultPayload{
				Status:  "unknown_error",
				Message: err.Error(),
			})
		}
	}
	c.Data(http.StatusOK, flashResultContentType, []byte(val))
}

func packFlashSaleItems(items []flashSaleItem) []flashSaleItem {
	if len(items) <= 1 {
		return items
	}
	merged := make(map[string]int32, len(items))
	order := make([]string, 0, len(items))
	for _, item := range items {
		if _, ok := merged[item.ID]; !ok {
			order = append(order, item.ID)
		}
		merged[item.ID] += item.Quantity
	}

	packed := make([]flashSaleItem, 0, len(order))
	for _, id := range order {
		packed = append(packed, flashSaleItem{
			ID:       id,
			Quantity: merged[id],
		})
	}
	return packed
}

func flashOnceOrderKey(customerID, itemID string) string {
	return flashOnceKeyPrefix + customerID + ":" + itemID
}

func mustMarshalFlashResult(payload flashResultPayload) string {
	b, err := json.Marshal(payload)
	if err != nil {
		return `{"status":"unknown_error","message":"failed to encode result payload"}`
	}
	return string(b)
}
