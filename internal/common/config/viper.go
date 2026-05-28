package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/spf13/viper"
)

func init() {
	if err := NewViperConfig(); err != nil {
		panic(fmt.Errorf("fatal error config file: %w", err))
	}
}

var once sync.Once

func NewViperConfig() (err error) {
	once.Do(func() {
		err = newViperConfig()
	})
	return
}

func newViperConfig() error {
	viper.SetConfigName("global")
	viper.SetConfigType("yaml")
	// Container / prod: explicit CONFIG_DIR wins.
	// Local dev: fall back to the source-file-relative hack.
	if dir := os.Getenv("CONFIG_DIR"); dir != "" {
		viper.AddConfigPath(dir)
	} else {
		relativePath, err := getRelativePathFromCaller()
		if err != nil {
			return err
		}
		viper.AddConfigPath(relativePath)
	}
	// Replace `.` and `-` with `_` so dotted config keys map to ENV_STYLE vars
	// (e.g. `mongo.host` ← MONGO_HOST, `order.http-addr` ← ORDER_HTTP_ADDR).
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))
	viper.AutomaticEnv()
	return viper.ReadInConfig()
}

func getRelativePathFromCaller() (relativePath string, err error) {
	callerPwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	_, here, _, _ := runtime.Caller(0)
	relativePath, err = filepath.Rel(callerPwd, filepath.Dir(here))
	return relativePath, err
}

func GetStringWithEnv(key string) string {
	return os.ExpandEnv(viper.GetString(key))
}
