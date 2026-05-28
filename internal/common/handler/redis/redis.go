package redis

import (
	"fmt"
	"time"

	"github.com/ecstasoy/gorder/common/handler/factory"
	"github.com/redis/go-redis/v9"
	"github.com/spf13/viper"
)

const (
	confName      = "redis"
	localSupplier = "local"
)

var (
	singleton = factory.NewSingleton(supplier)
)

func Init() {
	conf := viper.GetStringMap(confName)
	for supplierName := range conf {
		Client(supplierName)
	}
}

func LocalClient() *redis.Client {
	return Client(localSupplier)
}

func Client(name string) *redis.Client {
	return singleton.Get(name).(*redis.Client)
}

func supplier(key string) any {
	base := confName + "." + key // e.g. "redis.local"
	addr := fmt.Sprintf("%s:%s",
		viper.GetString(base+".ip"),
		viper.GetString(base+".port"),
	)
	connTimeoutMs := viper.GetInt(base + ".conn_timeout")
	readTimeoutMs := viper.GetInt(base + ".read_timeout")
	writeTimeoutMs := viper.GetInt(base + ".write_timeout")

	return redis.NewClient(&redis.Options{
		Network:         "tcp",
		Addr:            addr,
		PoolSize:        viper.GetInt(base + ".pool_size"),
		MaxActiveConns:  viper.GetInt(base + ".max_conn"),
		ConnMaxLifetime: time.Duration(connTimeoutMs) * time.Millisecond,
		ReadTimeout:     time.Duration(readTimeoutMs) * time.Millisecond,
		WriteTimeout:    time.Duration(writeTimeoutMs) * time.Millisecond,
	})
}
