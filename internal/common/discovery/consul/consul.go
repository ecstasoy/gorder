package consul

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/hashicorp/consul/api"
	"github.com/sirupsen/logrus"
)

type Registry struct {
	client *api.Client
}

var (
	consulClient *Registry
	once         sync.Once
	initErr      error
)

func New(consulAddr string) (*Registry, error) {
	once.Do(func() {
		config := api.DefaultConfig()
		config.Address = consulAddr
		client, err := api.NewClient(config)
		if err != nil {
			initErr = err
			return
		}
		consulClient = &Registry{client: client}
	})
	if initErr != nil {
		return nil, initErr
	}
	return consulClient, nil
}

func (r *Registry) Register(ctx context.Context, instanceID, serviceName, hostPort string) error {
	parts := strings.Split(hostPort, ":")
	if len(parts) != 2 {
		return fmt.Errorf("invalid hostPort format: %s", hostPort)
	}
	host := parts[0]
	port, _ := strconv.Atoi(parts[1])
	return r.client.Agent().ServiceRegister(&api.AgentServiceRegistration{
		ID:      instanceID,
		Name:    serviceName,
		Address: host,
		Port:    port,
		Check: &api.AgentServiceCheck{
			CheckID:                        instanceID,
			TLSSkipVerify:                  false,
			TTL:                            "5s",
			Timeout:                        "5s",
			DeregisterCriticalServiceAfter: "1m",
		},
	})
}

func (r *Registry) Deregister(ctx context.Context, instanceID, serviceName string) error {
	logrus.WithFields(logrus.Fields{
		"instance_id":  instanceID,
		"service_name": serviceName,
	}).Info("deregister service from Consul")
	return r.client.Agent().CheckDeregister(instanceID)
}

func (r *Registry) Discover(ctx context.Context, serviceName string) ([]string, error) {
	entries, _, err := r.client.Health().Service(serviceName, "", true, nil)
	if err != nil {
		return nil, err
	}
	var ips []string
	for _, entry := range entries {
		ips = append(ips, fmt.Sprintf("%s:%d", entry.Service.Address, entry.Service.Port))
	}
	return ips, nil
}

func (r *Registry) HealthCheck(instanceID, serviceName string) error {
	return r.client.Agent().UpdateTTL(instanceID, "TTL check: online", api.HealthPassing)
}
