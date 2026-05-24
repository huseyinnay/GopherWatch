package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Duration, YAML'daki "5s" gibi stringleri time.Duration'a çevirebilen wrapper.
// Bu olmadan yaml.v3, time.Duration'ı doğal olarak parse edemez.
type Duration time.Duration

func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	var s string
	if err := node.Decode(&s); err != nil {
		return err
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	*d = Duration(parsed)
	return nil
}

func (d Duration) Std() time.Duration { return time.Duration(d) }

type TargetType string

const (
	TargetHTTP TargetType = "http"
	TargetTCP  TargetType = "tcp"
)

type Global struct {
	LogLevel         string   `yaml:"log_level"`
	CheckInterval    Duration `yaml:"check_interval"`
	FailureThreshold int      `yaml:"failure_threshold"`
	RestartCooldown  Duration `yaml:"restart_cooldown"`
}

type Target struct {
	Name             string     `yaml:"name"`
	Type             TargetType `yaml:"type"`
	URL              string     `yaml:"url,omitempty"`
	Address          string     `yaml:"address,omitempty"`
	Method           string     `yaml:"method,omitempty"`
	ExpectedStatus   []int      `yaml:"expected_status,omitempty"`
	Timeout          Duration   `yaml:"timeout,omitempty"`
	CheckInterval    Duration   `yaml:"check_interval,omitempty"`
	FailureThreshold int        `yaml:"failure_threshold,omitempty"`
	Container        string     `yaml:"container,omitempty"`
	RestartCooldown  Duration   `yaml:"restart_cooldown,omitempty"`
}

type Config struct {
	Global  Global   `yaml:"global"`
	Targets []Target `yaml:"targets"`
}

// Load dosyayı okur, parse eder, varsayılanları uygular, doğrular.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	cfg.applyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}
	return &cfg, nil
}

func (c *Config) applyDefaults() {
	if c.Global.LogLevel == "" {
		c.Global.LogLevel = "info"
	}
	if c.Global.CheckInterval == 0 {
		c.Global.CheckInterval = Duration(10 * time.Second)
	}
	if c.Global.FailureThreshold == 0 {
		c.Global.FailureThreshold = 3
	}
	if c.Global.RestartCooldown == 0 {
		c.Global.RestartCooldown = Duration(60 * time.Second)
	}

	for i := range c.Targets {
		t := &c.Targets[i]
		if t.CheckInterval == 0 {
			t.CheckInterval = c.Global.CheckInterval
		}
		if t.FailureThreshold == 0 {
			t.FailureThreshold = c.Global.FailureThreshold
		}
		if t.RestartCooldown == 0 {
			t.RestartCooldown = c.Global.RestartCooldown
		}
		if t.Timeout == 0 {
			t.Timeout = Duration(5 * time.Second)
		}
		if t.Type == TargetHTTP {
			if t.Method == "" {
				t.Method = "GET"
			}
			if len(t.ExpectedStatus) == 0 {
				t.ExpectedStatus = []int{200}
			}
		}
	}
}

func (c *Config) Validate() error {
	if len(c.Targets) == 0 {
		return fmt.Errorf("en az bir target tanımlanmalı")
	}
	seen := make(map[string]bool)
	for i, t := range c.Targets {
		if t.Name == "" {
			return fmt.Errorf("target #%d: name zorunlu", i)
		}
		if seen[t.Name] {
			return fmt.Errorf("target adı tekrar etmiş: %s", t.Name)
		}
		seen[t.Name] = true

		switch t.Type {
		case TargetHTTP:
			if t.URL == "" {
				return fmt.Errorf("target %q: http tipi için url zorunlu", t.Name)
			}
		case TargetTCP:
			if t.Address == "" {
				return fmt.Errorf("target %q: tcp tipi için address zorunlu", t.Name)
			}
		default:
			return fmt.Errorf("target %q: bilinmeyen type %q", t.Name, t.Type)
		}
	}
	return nil
}
