package lite

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const defaultInlineOutputKB = 32

// LiteConfig is the minimal configuration subset for store-less skills.
type LiteConfig struct {
	Home           string             `json:"home" yaml:"home"`
	InlineOutputKB int                `json:"inline_output_kb" yaml:"inline_output_kb"`
	Paths          LitePaths          `json:"paths" yaml:"paths"`
	CAS            LiteCASPolicy      `json:"cas" yaml:"cas"`
	Search         LiteSearchSettings `json:"search" yaml:"search"`
}

// LitePaths holds on-disk locations rooted at the foxctl home directory.
type LitePaths struct {
	CAS   string `json:"cas" yaml:"cas"`
	Cache string `json:"cache" yaml:"cache"`
}

// LiteCASPolicy controls content-addressed storage behavior.
type LiteCASPolicy struct {
	Store  bool   `json:"store" yaml:"store"`
	Expose string `json:"expose" yaml:"expose"`
}

// LiteSearchSettings holds external search provider API keys.
type LiteSearchSettings struct {
	ExaAPIKey    string `json:"exa_api_key" yaml:"exa_api_key"`
	TavilyAPIKey string `json:"tavily_api_key" yaml:"tavily_api_key"`
}

// LoadConfig loads the lite configuration without importing the full
// internal/platform/config package.
func LoadConfig() (LiteConfig, error) {
	loadDotEnvLite()

	userHome, err := os.UserHomeDir()
	if err != nil {
		return LiteConfig{}, fmt.Errorf("config: resolve home: %w", err)
	}

	cfg := defaultLiteConfig(userHome)
	if err := readLiteConfigFile(&cfg, filepath.Join(cfg.Home, "config.yaml")); err != nil {
		return LiteConfig{}, err
	}
	applyLiteEnvOverrides(&cfg)
	normalizeLiteConfig(&cfg, userHome)
	return cfg, nil
}

func defaultLiteConfig(userHome string) LiteConfig {
	home := strings.TrimSpace(os.Getenv("FOXCTL_HOME"))
	if home == "" {
		home = filepath.Join(userHome, ".foxctl")
	}
	home = resolveLitePath(home, userHome, userHome)
	return LiteConfig{
		Home:           home,
		InlineOutputKB: defaultInlineOutputKB,
		Paths: LitePaths{
			CAS:   filepath.Join(home, "cas"),
			Cache: filepath.Join(home, "cache"),
		},
		CAS: LiteCASPolicy{
			Store:  true,
			Expose: "off",
		},
		Search: LiteSearchSettings{
			ExaAPIKey:    os.Getenv("EXA_API_KEY"),
			TavilyAPIKey: os.Getenv("TAVILY_API_KEY"),
		},
	}
}

type liteConfigFile struct {
	Home           string `yaml:"home"`
	InlineOutputKB int    `yaml:"inline_output_kb"`
	Paths          struct {
		CAS   string `yaml:"cas"`
		Cache string `yaml:"cache"`
	} `yaml:"paths"`
	CAS struct {
		Store  *bool  `yaml:"store"`
		Expose string `yaml:"expose"`
	} `yaml:"cas"`
	Search LiteSearchSettings `yaml:"search"`
}

func readLiteConfigFile(cfg *LiteConfig, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("config: read lite config: %w", err)
	}

	var fileCfg liteConfigFile
	if err := yaml.Unmarshal(data, &fileCfg); err != nil {
		return fmt.Errorf("config: decode lite config: %w", err)
	}

	if strings.TrimSpace(fileCfg.Home) != "" {
		cfg.Home = strings.TrimSpace(fileCfg.Home)
	}
	if fileCfg.InlineOutputKB > 0 {
		cfg.InlineOutputKB = fileCfg.InlineOutputKB
	}
	if strings.TrimSpace(fileCfg.Paths.CAS) != "" {
		cfg.Paths.CAS = strings.TrimSpace(fileCfg.Paths.CAS)
	}
	if strings.TrimSpace(fileCfg.Paths.Cache) != "" {
		cfg.Paths.Cache = strings.TrimSpace(fileCfg.Paths.Cache)
	}
	if fileCfg.CAS.Store != nil {
		cfg.CAS.Store = *fileCfg.CAS.Store
	}
	if expose, ok := normalizeCASExpose(fileCfg.CAS.Expose); ok {
		cfg.CAS.Expose = expose
	}
	if strings.TrimSpace(fileCfg.Search.ExaAPIKey) != "" {
		cfg.Search.ExaAPIKey = strings.TrimSpace(fileCfg.Search.ExaAPIKey)
	}
	if strings.TrimSpace(fileCfg.Search.TavilyAPIKey) != "" {
		cfg.Search.TavilyAPIKey = strings.TrimSpace(fileCfg.Search.TavilyAPIKey)
	}

	return nil
}

func applyLiteEnvOverrides(cfg *LiteConfig) {
	if v := strings.TrimSpace(os.Getenv("FOXCTL_HOME")); v != "" {
		cfg.Home = v
	}
	if v := strings.TrimSpace(os.Getenv("FOXCTL_INLINE_OUTPUT_KB")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.InlineOutputKB = n
		}
	}
	if v := strings.TrimSpace(os.Getenv("FOXCTL_PATHS_CAS")); v != "" {
		cfg.Paths.CAS = v
	}
	if v := strings.TrimSpace(os.Getenv("FOXCTL_PATHS_CACHE")); v != "" {
		cfg.Paths.Cache = v
	}
	if v := strings.TrimSpace(os.Getenv("FOXCTL_CAS_STORE")); v != "" {
		cfg.CAS.Store = parseBool(v, cfg.CAS.Store)
	}
	if expose, ok := normalizeCASExpose(os.Getenv("FOXCTL_CAS_EXPOSE")); ok {
		cfg.CAS.Expose = expose
	}
	if key := strings.TrimSpace(os.Getenv("EXA_API_KEY")); key != "" {
		cfg.Search.ExaAPIKey = key
	}
	if key := strings.TrimSpace(os.Getenv("TAVILY_API_KEY")); key != "" {
		cfg.Search.TavilyAPIKey = key
	}
}

func normalizeLiteConfig(cfg *LiteConfig, userHome string) {
	if strings.TrimSpace(cfg.Home) == "" {
		cfg.Home = filepath.Join(userHome, ".foxctl")
	}
	cfg.Home = resolveLitePath(cfg.Home, userHome, userHome)
	if cfg.InlineOutputKB <= 0 {
		cfg.InlineOutputKB = defaultInlineOutputKB
	}
	if strings.TrimSpace(cfg.Paths.CAS) == "" {
		cfg.Paths.CAS = filepath.Join(cfg.Home, "cas")
	}
	if strings.TrimSpace(cfg.Paths.Cache) == "" {
		cfg.Paths.Cache = filepath.Join(cfg.Home, "cache")
	}
	cfg.Paths.CAS = resolveLitePath(cfg.Paths.CAS, cfg.Home, userHome)
	cfg.Paths.Cache = resolveLitePath(cfg.Paths.Cache, cfg.Home, userHome)
	if expose, ok := normalizeCASExpose(cfg.CAS.Expose); ok {
		cfg.CAS.Expose = expose
	} else {
		cfg.CAS.Expose = "off"
	}
}

func resolveLitePath(path, base, userHome string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if strings.HasPrefix(path, "~") {
		trimmed := strings.TrimPrefix(path, "~")
		trimmed = strings.TrimPrefix(trimmed, string(filepath.Separator))
		return filepath.Join(userHome, trimmed)
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(base, path)
}

func normalizeCASExpose(value string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "off", "0", "false":
		return "off", true
	case "digest":
		return "digest", true
	case "hint":
		return "hint", true
	default:
		return "", false
	}
}

func envBool(key string, fallback bool) bool {
	return parseBool(os.Getenv(key), fallback)
}

func parseBool(value string, fallback bool) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

// loadDotEnvLite mimics internal/platform/config.LoadDotEnv:
//  1. ~/.foxctl/.env
//  2. $FOXCTL_HOME/.env (if different)
//  3. $PWD/.env
//
// Files are loaded in reverse order, matching godotenv.Load's non-overriding
// behavior so project-level values win over global defaults.
func loadDotEnvLite() {
	var files []string

	if home, err := os.UserHomeDir(); err == nil {
		files = append(files, filepath.Join(home, ".foxctl", ".env"))
	}

	if foxctlHome := strings.TrimSpace(os.Getenv("FOXCTL_HOME")); foxctlHome != "" {
		f := filepath.Join(foxctlHome, ".env")
		if len(files) == 0 || files[0] != f {
			files = append(files, f)
		}
	}

	if cwd, err := os.Getwd(); err == nil {
		files = append(files, filepath.Join(cwd, ".env"))
	}

	for i := len(files) - 1; i >= 0; i-- {
		loadEnvFile(files[i])
	}
}

func loadEnvFile(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := parseEnvLine(line)
		if !ok {
			continue
		}
		if os.Getenv(key) == "" {
			_ = os.Setenv(key, val)
		}
	}
}

func parseEnvLine(line string) (string, string, bool) {
	idx := strings.Index(line, "=")
	if idx < 0 {
		return "", "", false
	}
	key := strings.TrimSpace(line[:idx])
	if key == "" {
		return "", "", false
	}
	val := strings.TrimSpace(line[idx+1:])
	if len(val) >= 2 {
		if (val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'') {
			val = val[1 : len(val)-1]
		}
	}
	return key, val, true
}
