package main

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Config struct {
	ListenAddr       string
	UpstreamBaseURL  string
	AuthTokens       []string
	RotationInterval time.Duration
	RequestTimeout   time.Duration
	StreamTimeout    time.Duration
	UserAgent        string
	APIKeys          []string
	HTTPProxy        string
	AdminPassword    string
	ModelAliases     map[string]string
	Policy           PolicyConfig
}

type rawConfig struct {
	ListenAddr       string            `json:"LISTEN_ADDR"`
	UpstreamBaseURL  string            `json:"UPSTREAM_BASE_URL"`
	AuthTokens       []string          `json:"AUTH_TOKENS"`
	RotationInterval string            `json:"ROTATION_INTERVAL"`
	RequestTimeout   string            `json:"REQUEST_TIMEOUT"`
	StreamTimeout    string            `json:"STREAM_TIMEOUT"`
	APIKeys          []string          `json:"API_KEYS"`
	HTTPProxy        string            `json:"HTTP_PROXY"`
	AdminPassword    string            `json:"ADMIN_PASSWORD"`
	ModelAliases     map[string]string `json:"MODEL_ALIASES"`
	Policy           rawPolicyConfig   `json:"POLICY"`
}

type PolicyConfig struct {
	MaxRetries             int
	RetryBackoffBase       time.Duration
	RetryBackoffMax        time.Duration
	PerTokenConcurrency    int
	HealthCheckEnabled     bool
	HealthCheckInterval    time.Duration
	HealthFailureThreshold int
	RoutingMode            string
	PriorityFailoverStep   int
}

type rawPolicyConfig struct {
	MaxRetries             *int   `json:"MAX_RETRIES"`
	RetryBackoffBase       string `json:"RETRY_BACKOFF_BASE"`
	RetryBackoffMax        string `json:"RETRY_BACKOFF_MAX"`
	PerTokenConcurrency    *int   `json:"PER_TOKEN_CONCURRENCY"`
	HealthCheckEnabled     *bool  `json:"HEALTH_CHECK_ENABLED"`
	HealthCheckInterval    string `json:"HEALTH_CHECK_INTERVAL"`
	HealthFailureThreshold *int   `json:"HEALTH_FAILURE_THRESHOLD"`
	RoutingMode            string `json:"ROUTING_MODE"`
	PriorityFailoverStep   *int   `json:"PRIORITY_FAILOVER_STEP"`
}

func loadConfig(configPath string) (Config, error) {
	cfg, err := loadRawConfig(configPath)
	if err != nil {
		return Config{}, err
	}

	overrideString(&cfg.ListenAddr, "LISTEN_ADDR")
	overrideString(&cfg.UpstreamBaseURL, "UPSTREAM_BASE_URL")
	overrideString(&cfg.RotationInterval, "ROTATION_INTERVAL")
	overrideString(&cfg.RequestTimeout, "REQUEST_TIMEOUT")
	overrideString(&cfg.StreamTimeout, "STREAM_TIMEOUT")
	overrideCSV(&cfg.AuthTokens, "AUTH_TOKENS")
	overrideCSV(&cfg.APIKeys, "API_KEYS")
	overrideString(&cfg.HTTPProxy, "HTTP_PROXY")
	overrideString(&cfg.AdminPassword, "ADMIN_PASSWORD")

	rotationInterval, err := time.ParseDuration(strings.TrimSpace(cfg.RotationInterval))
	if err != nil {
		return Config{}, fmt.Errorf("parse rotation interval: %w", err)
	}

	requestTimeout, err := time.ParseDuration(strings.TrimSpace(cfg.RequestTimeout))
	if err != nil {
		return Config{}, fmt.Errorf("parse request timeout: %w", err)
	}
	streamTimeoutRaw := strings.TrimSpace(cfg.StreamTimeout)
	if streamTimeoutRaw == "" {
		streamTimeoutRaw = cfg.RequestTimeout
	}
	streamTimeout, err := time.ParseDuration(streamTimeoutRaw)
	if err != nil {
		return Config{}, fmt.Errorf("parse stream timeout: %w", err)
	}
	policyCfg, err := parsePolicyConfig(cfg.Policy)
	if err != nil {
		return Config{}, err
	}

	finalCfg := Config{
		ListenAddr:       strings.TrimSpace(cfg.ListenAddr),
		UpstreamBaseURL:  strings.TrimRight(strings.TrimSpace(cfg.UpstreamBaseURL), "/"),
		AuthTokens:       dedupeStrings(cfg.AuthTokens),
		RotationInterval: rotationInterval,
		RequestTimeout:   requestTimeout,
		StreamTimeout:    streamTimeout,
		UserAgent:        generateUserAgent(),
		APIKeys:          dedupeStrings(cfg.APIKeys),
		HTTPProxy:        strings.TrimSpace(cfg.HTTPProxy),
		AdminPassword:    strings.TrimSpace(cfg.AdminPassword),
		ModelAliases:     normalizeModelAliases(cfg.ModelAliases),
		Policy:           policyCfg,
	}

	switch {
	case finalCfg.ListenAddr == "":
		return Config{}, errors.New("LISTEN_ADDR cannot be empty")
	case finalCfg.UpstreamBaseURL == "":
		return Config{}, errors.New("UPSTREAM_BASE_URL cannot be empty")
	case finalCfg.RotationInterval <= 0:
		return Config{}, errors.New("ROTATION_INTERVAL must be greater than zero")
	case finalCfg.RequestTimeout <= 0:
		return Config{}, errors.New("REQUEST_TIMEOUT must be greater than zero")
	case finalCfg.StreamTimeout <= 0:
		return Config{}, errors.New("STREAM_TIMEOUT must be greater than zero")
	}

	return finalCfg, nil
}

func loadRawConfig(configPath string) (rawConfig, error) {
	cfg := rawConfig{
		ListenAddr:       ":8080",
		UpstreamBaseURL:  "https://codebuff.com",
		RotationInterval: "6h",
		RequestTimeout:   "15m",
		StreamTimeout:    "15m",
		Policy: rawPolicyConfig{
			MaxRetries:             intPtr(2),
			RetryBackoffBase:       "500ms",
			RetryBackoffMax:        "6s",
			PerTokenConcurrency:    intPtr(8),
			HealthCheckEnabled:     boolPtr(true),
			HealthCheckInterval:    "3m",
			HealthFailureThreshold: intPtr(3),
		},
	}

	if configPath != "" {
		path, err := filepath.Abs(configPath)
		if err != nil {
			return rawConfig{}, fmt.Errorf("resolve config path: %w", err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return rawConfig{}, fmt.Errorf("read config file: %w", err)
		}
		if err := json.Unmarshal(data, &cfg); err != nil {
			return rawConfig{}, fmt.Errorf("parse config file: %w", err)
		}
	}

	return cfg, nil
}

func overrideString(target *string, envName string) {
	if value := strings.TrimSpace(os.Getenv(envName)); value != "" {
		*target = value
	}
}

func overrideCSV(target *[]string, envName string) {
	value := strings.TrimSpace(os.Getenv(envName))
	if value == "" {
		return
	}
	*target = splitList(value)
}

func splitList(value string) []string {
	fields := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r'
	})
	return compactStrings(fields)
}

func compactStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		out = append(out, value)
	}
	return out
}

func dedupeStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range compactStrings(values) {
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func generateUserAgent() string {
	return "ai-sdk/openai-compatible/1.0.25/codebuff"
}

func parsePolicyConfig(raw rawPolicyConfig) (PolicyConfig, error) {
	result := PolicyConfig{
		MaxRetries:             2,
		RetryBackoffBase:       500 * time.Millisecond,
		RetryBackoffMax:        6 * time.Second,
		PerTokenConcurrency:    8,
		HealthCheckEnabled:     true,
		HealthCheckInterval:    3 * time.Minute,
		HealthFailureThreshold: 3,
		RoutingMode:            routingModeRoundRobin,
		PriorityFailoverStep:   defaultPriorityFailoverStep,
	}
	if raw.MaxRetries != nil {
		result.MaxRetries = *raw.MaxRetries
	}
	if raw.PerTokenConcurrency != nil {
		result.PerTokenConcurrency = *raw.PerTokenConcurrency
	}
	if raw.HealthCheckEnabled != nil {
		result.HealthCheckEnabled = *raw.HealthCheckEnabled
	}
	if raw.HealthFailureThreshold != nil {
		result.HealthFailureThreshold = *raw.HealthFailureThreshold
	}
	if raw.PriorityFailoverStep != nil {
		result.PriorityFailoverStep = *raw.PriorityFailoverStep
	}
	routingMode := strings.TrimSpace(raw.RoutingMode)
	if routingMode != "" {
		result.RoutingMode = routingMode
	}
	if strings.TrimSpace(raw.RetryBackoffBase) != "" {
		d, err := time.ParseDuration(strings.TrimSpace(raw.RetryBackoffBase))
		if err != nil {
			return PolicyConfig{}, fmt.Errorf("parse policy retry backoff base: %w", err)
		}
		result.RetryBackoffBase = d
	}
	if strings.TrimSpace(raw.RetryBackoffMax) != "" {
		d, err := time.ParseDuration(strings.TrimSpace(raw.RetryBackoffMax))
		if err != nil {
			return PolicyConfig{}, fmt.Errorf("parse policy retry backoff max: %w", err)
		}
		result.RetryBackoffMax = d
	}
	if strings.TrimSpace(raw.HealthCheckInterval) != "" {
		d, err := time.ParseDuration(strings.TrimSpace(raw.HealthCheckInterval))
		if err != nil {
			return PolicyConfig{}, fmt.Errorf("parse policy health check interval: %w", err)
		}
		result.HealthCheckInterval = d
	}
	if result.MaxRetries < 0 {
		return PolicyConfig{}, errors.New("POLICY.MAX_RETRIES cannot be negative")
	}
	if result.RetryBackoffBase <= 0 {
		return PolicyConfig{}, errors.New("POLICY.RETRY_BACKOFF_BASE must be greater than zero")
	}
	if result.RetryBackoffMax < result.RetryBackoffBase {
		return PolicyConfig{}, errors.New("POLICY.RETRY_BACKOFF_MAX must be greater than or equal to RETRY_BACKOFF_BASE")
	}
	if result.PerTokenConcurrency <= 0 {
		return PolicyConfig{}, errors.New("POLICY.PER_TOKEN_CONCURRENCY must be greater than zero")
	}
	if result.HealthCheckInterval <= 0 {
		return PolicyConfig{}, errors.New("POLICY.HEALTH_CHECK_INTERVAL must be greater than zero")
	}
	if result.HealthFailureThreshold <= 0 {
		return PolicyConfig{}, errors.New("POLICY.HEALTH_FAILURE_THRESHOLD must be greater than zero")
	}
	result.RoutingMode = normalizeRoutingMode(result.RoutingMode)
	if result.PriorityFailoverStep <= 0 {
		return PolicyConfig{}, errors.New("POLICY.PRIORITY_FAILOVER_STEP must be greater than zero")
	}
	return result, nil
}

func normalizeModelAliases(input map[string]string) map[string]string {
	if len(input) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(input))
	for alias, model := range input {
		alias = strings.TrimSpace(alias)
		model = strings.TrimSpace(model)
		if alias == "" || model == "" {
			continue
		}
		out[alias] = model
	}
	return out
}

func intPtr(value int) *int {
	return &value
}

func boolPtr(value bool) *bool {
	return &value
}

// generateClientSessionId generates a per-request session ID matching the
// official SDK: Math.random().toString(36).substring(2, 15) — a ~13-char
// base-36 alphanumeric string.
func generateClientSessionId() string {
	buf := make([]byte, 10)
	if _, err := rand.Read(buf); err != nil {
		buf = []byte(fmt.Sprintf("%d", time.Now().UnixNano()))
	}
	const alphabet = "0123456789abcdefghijklmnopqrstuvwxyz"
	out := make([]byte, 13)
	for i := range out {
		out[i] = alphabet[buf[i%len(buf)]%36]
	}
	return string(out)
}
