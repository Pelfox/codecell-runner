package pkg

import (
	"strings"

	"github.com/spf13/viper"
)

// RuntimeType represents the type of container runtime to use.
type RuntimeType string

const (
	// RuntimeTypeDocker represents the default Docker runtime (runc).
	RuntimeTypeDocker RuntimeType = "docker"
	// RuntimeTypeGvisor represents the gVisor runtime.
	RuntimeTypeGvisor RuntimeType = "gvisor"
)

// AppConfig holds the configuration settings for the application.
type AppConfig struct {
	// Addr is the address to start the gRPC server on.
	Addr string `mapstructure:"addr"`
	// Runtime is the container runtime to use.
	Runtime RuntimeType `mapstructure:"runtime"`
	// EnableStorageOpt indicates whether to enable storage optimizations.
	EnableStorageOpt bool `mapstructure:"enable_storage_opt"`
	// MemoryLimit is the memory limit for containers in bytes.
	MemoryLimit int64 `mapstructure:"memory_limit"`
	// CPULimit is the CPU limit for containers in nanos.
	CPULimit int64 `mapstructure:"cpu_limit"`
}

// LoadConfig loads the application configuration from environment variables
// and sets default values for missing settings.
func LoadConfig() (*AppConfig, error) {
	v := viper.New()

	v.AutomaticEnv()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	// setting default values
	v.SetDefault("addr", ":50051")
	v.SetDefault("runtime", RuntimeTypeDocker)
	v.SetDefault("enable_storage_opt", false)
	v.SetDefault("memory_limit", 512*1024*1024)
	v.SetDefault("cpu_limit", 1_000_000_000)

	var config AppConfig
	if err := v.Unmarshal(&config); err != nil {
		return nil, err
	}
	return &config, nil
}
