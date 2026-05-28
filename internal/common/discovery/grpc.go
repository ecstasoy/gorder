package discovery

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"github.com/ecstasoy/gorder/common/discovery/consul"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

func RegisterToConsul(ctx context.Context, serviceName string) (func() error, error) {
	registry, err := consul.New(viper.GetString("consul.addr"))
	if err != nil {
		return func() error { return nil }, err
	}
	instanceID := GenerateInstanceID(serviceName)
	// registerAddr is what peers use to reach us (e.g. "stock:5003" in
	// docker-compose, "$POD_IP:5003" in K8s). Falls back to grpc-addr so
	// local dev keeps working without extra config.
	sub := viper.Sub(serviceName)
	registerAddr := sub.GetString("grpc-register-addr")
	if registerAddr == "" {
		registerAddr = sub.GetString("grpc-addr")
	}
	if err := registry.Register(ctx, instanceID, serviceName, registerAddr); err != nil {
		return func() error { return nil }, err
	}

	go func() {
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if err := registry.HealthCheck(instanceID, serviceName); err != nil {
					logrus.Errorf("health check failed for instance %s of service %s: %v", instanceID, serviceName, err)
				}
			case <-ctx.Done():
				logrus.Info("stopping health check goroutine")
				return
			}
		}
	}()

	logrus.WithFields(logrus.Fields{
		"serviceName": serviceName,
		"addr":        registerAddr,
		"instanceID":  instanceID,
	}).Info("registered to consul")

	return func() error {
		return registry.Deregister(ctx, instanceID, serviceName)
	}, nil
}

func GetServiceAddr(ctx context.Context, serviceName string) (string, error) {
	registry, err := consul.New(viper.GetString("consul.addr"))
	if err != nil {
		return "", err
	}
	addrs, err := registry.Discover(ctx, serviceName)
	if err != nil {
		return "", err
	}
	if len(addrs) == 0 {
		return "", fmt.Errorf("service %s not found", serviceName)
	}
	i := rand.Intn(len(addrs))
	logrus.Infof("discovered %d instances for service %s, selected instance: %s", len(addrs), serviceName, addrs[i])
	return addrs[i], nil
}
