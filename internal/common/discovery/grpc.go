package discovery

import (
	"context"
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
	hostPort := viper.Sub(serviceName).GetString("grpc-addr")
	if err := registry.Register(ctx, instanceID, serviceName, hostPort); err != nil {
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
		"addr":        hostPort,
		"instanceID":  instanceID,
	}).Info("registered to consul")

	return func() error {
		return registry.Deregister(ctx, instanceID, serviceName)
	}, nil
}
