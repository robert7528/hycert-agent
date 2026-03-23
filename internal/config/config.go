package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/viper"
)

type Config struct {
	Server ServerConfig `mapstructure:"server"`
	Agent  AgentConfig  `mapstructure:"agent"`
	Log    LogConfig    `mapstructure:"log"`
}

type ServerConfig struct {
	URL                string `mapstructure:"url"`
	Token              string `mapstructure:"token"`
	InsecureSkipVerify bool   `mapstructure:"insecure_skip_verify"`
}

type AgentConfig struct {
	Hostname  string `mapstructure:"hostname"`
	Interval  int    `mapstructure:"interval"`
	Backup    bool   `mapstructure:"backup"`
	BackupDir string `mapstructure:"backup_dir"`
}

type LogConfig struct {
	Level      string `mapstructure:"level"`
	File       string `mapstructure:"file"`
	MaxSize    int    `mapstructure:"max_size"`    // MB per file before rotation
	MaxBackups int    `mapstructure:"max_backups"` // number of old files to keep
	MaxAge     int    `mapstructure:"max_age"`     // days to retain old files
	Compress   bool   `mapstructure:"compress"`    // gzip old files
}

// Load reads config from file and env overrides.
func Load(cfgFile string) (*Config, error) {
	v := viper.New()

	// Defaults
	v.SetDefault("agent.interval", 3600)
	v.SetDefault("agent.backup", true)
	v.SetDefault("log.level", "info")
	v.SetDefault("log.max_size", 10)
	v.SetDefault("log.max_backups", 3)
	v.SetDefault("log.max_age", 30)
	v.SetDefault("log.compress", true)

	if cfgFile != "" {
		v.SetConfigFile(cfgFile)
	} else {
		// Try default locations
		v.SetConfigName("agent")
		v.SetConfigType("yaml")
		v.AddConfigPath("/etc/hycert")
		v.AddConfigPath("C:\\hycert")
		v.AddConfigPath(".")
	}

	// Env overrides: HYCERT_AGENT_TOKEN, HYCERT_SERVER_URL, etc.
	v.SetEnvPrefix("HYCERT")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// Token env has special name for convenience
	if token := os.Getenv("HYCERT_AGENT_TOKEN"); token != "" {
		v.Set("server.token", token)
	}

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("read config: %w", err)
		}
		// Config file not found is OK if env vars provide everything
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	if cfg.Server.URL == "" {
		return nil, fmt.Errorf("server.url is required")
	}
	if cfg.Server.Token == "" {
		return nil, fmt.Errorf("server.token is required (set in config or HYCERT_AGENT_TOKEN env)")
	}

	// Auto-detect hostname
	if cfg.Agent.Hostname == "" {
		h, err := os.Hostname()
		if err != nil {
			return nil, fmt.Errorf("detect hostname: %w", err)
		}
		cfg.Agent.Hostname = h
	}

	return &cfg, nil
}
