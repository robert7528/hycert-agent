package config

import (
	"bufio"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/google/uuid"
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
	Proxy              string `mapstructure:"proxy"`
	InsecureSkipVerify bool   `mapstructure:"insecure_skip_verify"`
}

type AgentConfig struct {
	AgentID   string `mapstructure:"agent_id"`
	Name      string `mapstructure:"name"`
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

	// Auto-generate agent_id if not set, with machine-id dedup
	if cfg.Agent.AgentID == "" {
		idFile := agentIDFilePath(cfgFile)
		storedUUID, storedMachine := readAgentIDFile(idFile)
		currentMachine := getMachineID()

		if storedUUID == "" {
			// No file or unreadable → generate new
			cfg.Agent.AgentID = uuid.New().String()
			writeAgentIDFile(idFile, cfg.Agent.AgentID, currentMachine)
		} else if storedMachine == "" {
			// Old format (no machine line) → migrate: keep UUID, write machine-id
			cfg.Agent.AgentID = storedUUID
			writeAgentIDFile(idFile, storedUUID, currentMachine)
		} else if currentMachine == "" {
			// Cannot read current machine-id → keep UUID, log warning
			cfg.Agent.AgentID = storedUUID
			slog.Warn("cannot read machine-id, keeping existing agent-id")
		} else if storedMachine == currentMachine {
			// Same machine → keep UUID
			cfg.Agent.AgentID = storedUUID
		} else {
			// Different machine (copied config) → generate new UUID
			cfg.Agent.AgentID = uuid.New().String()
			writeAgentIDFile(idFile, cfg.Agent.AgentID, currentMachine)
			slog.Info("detected different machine, generated new agent-id",
				"old_machine", storedMachine, "new_machine", currentMachine,
				"new_agent_id", cfg.Agent.AgentID)
		}
	}

	// Auto-detect name from hostname if not set
	if cfg.Agent.Name == "" {
		cfg.Agent.Name = cfg.Agent.Hostname
	}

	return &cfg, nil
}

// agentIDFilePath returns the path to the persistent agent-id file.
// On Linux: /etc/hycert/agent-id
// On Windows: next to config file, or C:\hycert\agent-id
func agentIDFilePath(cfgFile string) string {
	if cfgFile != "" {
		return filepath.Join(filepath.Dir(cfgFile), "agent-id")
	}
	if runtime.GOOS == "windows" {
		return `C:\hycert\agent-id`
	}
	return "/etc/hycert/agent-id"
}

// readAgentIDFile reads the agent-id file and returns (agentID, storedMachineID).
// File format:
//
//	line 1: UUID
//	line 2: machine:{machine-id}  (optional, old format has no line 2)
func readAgentIDFile(path string) (agentID string, machineID string) {
	f, err := os.Open(path)
	if err != nil {
		return "", ""
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	if scanner.Scan() {
		agentID = strings.TrimSpace(scanner.Text())
	}
	if scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "machine:") {
			machineID = strings.TrimPrefix(line, "machine:")
		}
	}
	return agentID, machineID
}

// writeAgentIDFile writes the agent-id and machine-id to the file.
func writeAgentIDFile(path string, agentID string, machineID string) {
	os.MkdirAll(filepath.Dir(path), 0755)
	content := agentID + "\n"
	if machineID != "" {
		content += "machine:" + machineID + "\n"
	}
	os.WriteFile(path, []byte(content), 0600)
}
