package adapters

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/ecstasoy/gorder/common/genproto/orderpb"
	domain "github.com/ecstasoy/gorder/order/domain/order"
	"github.com/sirupsen/logrus"
)

type MemoryOrderRepository struct {
	lock  *sync.RWMutex
	store []*domain.Order
}

var testData = []*domain.Order{}

func NewMemoryOrderRepository() *MemoryOrderRepository {
	s := make([]*domain.Order, 0)
	s = append(s, &domain.Order{
		ID:          "foo_ID",
		CustomerID:  "foo_customer_ID",
		Status:      orderpb.OrderStatus_ORDER_STATUS_PENDING,
		PaymentLink: "foo_payment_link",
		Items:       nil,
	})
	return &MemoryOrderRepository{
		lock:  &sync.RWMutex{},
		store: s,
	}
}

func (m *MemoryOrderRepository) Create(_ context.Context, order *domain.Order) (*domain.Order, error) {
	m.lock.Lock()
	defer m.lock.Unlock()

	res := &domain.Order{
		ID:          strconv.FormatInt(time.Now().UnixNano(), 10),
		CustomerID:  order.CustomerID,
		Status:      order.Status,
		PaymentLink: order.PaymentLink,
		Items:       order.Items,
	}
	m.store = append(m.store, res)

	logrus.WithFields(logrus.Fields{
		"order_id":           res.ID,
		"store_after_create": m.store,
	}).Debug("order created in memory repository")

	return res, nil
}

func (m *MemoryOrderRepository) Get(_ context.Context, id, customerID string) (*domain.Order, error) {
	m.lock.RLock()
	defer m.lock.RUnlock()

	for _, o := range m.store {
		if o.ID == id && o.CustomerID == customerID {
			logrus.WithFields(logrus.Fields{
				"order_id":    id,
				"customer_id": customerID,
			}).Debug("order found in memory repository")
			return o, nil
		}
	}

	return nil, &domain.NotFoundError{OrderID: id}
}

func (m *MemoryOrderRepository) Update(ctx context.Context, order *domain.Order, updateFunc func(context.Context, *domain.Order) (*domain.Order, error)) error {
	m.lock.Lock()
	defer m.lock.Unlock()

	found := false
	for i, o := range m.store {
		if o.ID == order.ID && o.CustomerID == order.CustomerID {
			found = true
			updateOrder, err := updateFunc(ctx, o)
			if err != nil {
				return err
			}
			m.store[i] = updateOrder

			logrus.WithFields(logrus.Fields{
				"order_id":    order.ID,
				"customer_id": order.CustomerID,
			}).Debug("order updated in memory repository")

			break
		}
	}

	if !found {
		return &domain.NotFoundError{OrderID: order.ID}
	}

	return nil
}
