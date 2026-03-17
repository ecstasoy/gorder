package config

import (
	"os"
	"strings"

	"github.com/spf13/viper"
)

func NewViperConfig() error {
	viper.SetConfigName("global")
	viper.SetConfigType("yaml")
	viper.AddConfigPath("../common/config")
	viper.EnvKeyReplacer(strings.NewReplacer("_", "-"))
	viper.AutomaticEnv()
	return viper.ReadInConfig()
}

func GetStringWithEnv(key string) string {
	return os.ExpandEnv(viper.GetString(key))
}
