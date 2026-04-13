package config

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/viper"
)

// Config holds all runtime configuration.
type Config struct {
	// Required
	SpecPath   string
	MountPoint string

	// Connection
	BaseURL string
	Timeout time.Duration

	// Auth
	AuthBearer      string
	AuthBasic       string
	AuthAPIKey      string
	AuthAPIKeyParam string

	// Caching
	CacheTTL       time.Duration
	CacheMaxSizeMB int

	// Behaviour
	DryRun         bool
	Verbose         bool
	ReadOnly        bool
	AllowOther      bool
	PrettyJSON      bool
	ResponseFormat  string

	// Grouping strategy
	GroupBy string

	// Profile name (from config file)
	Profile string
}

// Load creates a Config from viper's bound values.
func Load(v *viper.Viper) (*Config, error) {
	cfg := &Config{
		SpecPath:        v.GetString("spec"),
		MountPoint:      v.GetString("mount"),
		BaseURL:         v.GetString("base-url"),
		Timeout:         v.GetDuration("timeout"),
		AuthBearer:      v.GetString("auth-bearer"),
		AuthBasic:       v.GetString("auth-basic"),
		AuthAPIKey:      v.GetString("auth-apikey"),
		AuthAPIKeyParam: v.GetString("auth-apikey-param"),
		CacheTTL:        v.GetDuration("cache-ttl"),
		CacheMaxSizeMB:  v.GetInt("cache-max-mb"),
		DryRun:          v.GetBool("dry-run"),
		Verbose:         v.GetBool("verbose"),
		ReadOnly:        v.GetBool("read-only"),
		AllowOther:      v.GetBool("allow-other"),
		PrettyJSON:      v.GetBool("pretty"),
		ResponseFormat:  v.GetString("response-format"),
		GroupBy:         v.GetString("group-by"),
		Profile:         v.GetString("profile"),
	}

	// Apply defaults
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.CacheTTL == 0 && !v.IsSet("cache-ttl") {
		cfg.CacheTTL = 30 * time.Second
	}
	if cfg.CacheMaxSizeMB == 0 {
		cfg.CacheMaxSizeMB = 50
	}
	if cfg.GroupBy == "" {
		cfg.GroupBy = "tags"
	}
	if cfg.ResponseFormat == "" {
		cfg.ResponseFormat = "json"
	}
	if !v.IsSet("pretty") {
		cfg.PrettyJSON = true
	}

	return cfg, nil
}

// Validate checks that required fields are set and the mount point is valid.
func (c *Config) Validate() error {
	if c.SpecPath == "" {
		return fmt.Errorf("--spec is required")
	}
	if c.MountPoint == "" && !c.DryRun {
		return fmt.Errorf("--mount is required (or use --dry-run)")
	}

	if c.MountPoint != "" {
		info, err := os.Stat(c.MountPoint)
		if err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("mount point does not exist: %s", c.MountPoint)
			}
			return fmt.Errorf("cannot access mount point: %w", err)
		}
		if !info.IsDir() {
			return fmt.Errorf("mount point is not a directory: %s", c.MountPoint)
		}
	}

	return nil
}
