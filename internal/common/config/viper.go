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
	relativePath, err := getRelativePathFromCaller()
	if err != nil {
		return err
	}
	viper.SetConfigName("global")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(relativePath)
	viper.EnvKeyReplacer(strings.NewReplacer("_", "-"))
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
	fmt.Printf("caller from: %s, here: %s, relpath: %s\n", callerPwd, here, relativePath)
	return relativePath, err
}

func GetStringWithEnv(key string) string {
	return os.ExpandEnv(viper.GetString(key))
}
