package integration

import (
	"context"

	"github.com/ecstasoy/gorder/common/config"
	"github.com/stripe/stripe-go/v80"
	"github.com/stripe/stripe-go/v80/product"
)

type StripeAPI struct {
	apiKey string
}

func NewStripeAPI() *StripeAPI {
	key := config.GetStringWithEnv("stripe-key")
	return &StripeAPI{
		apiKey: key,
	}
}

func (s *StripeAPI) GetPriceByProductID(ctx context.Context, productID string) (string, error) {
	stripe.Key = s.apiKey
	result, err := product.Get(productID, &stripe.ProductParams{})
	if err != nil {
		return "", err
	}
	return result.DefaultPrice.ID, err
}

func (s *StripeAPI) GetProductByID(ctx context.Context, pid string) (*stripe.Product, error) {
	stripe.Key = s.apiKey
	return product.Get(pid, &stripe.ProductParams{})
}
