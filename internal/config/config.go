package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const FileName = "gossr.yaml"

var defaultConfig = Config{
	WebDir:           "./internal/web",
	GenDataProviders: true,
	GoRunArgs:        ".",
}

type Config struct {
	Dir              string            `yaml:"-"`
	Prod             bool              `yaml:"-"`
	WebDir           string            `yaml:"webDir"`           // Directory containing SSR handlers and templates
	WebPackage       string            `yaml:"webPackage"`       // Full path to the web package
	GoRunArgs        string            `yaml:"goRunArgs"`        // Arguments for `go run`
	Env              map[string]string `yaml:"env"`              // Environment variables
	GenDataProviders bool              `yaml:"genDataProviders"` // Enable basic DataProviders implementation generation (experimental)
}

func Read() (*Config, error) {
	curDir, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	for curDir != "/" {
		configPath := filepath.Join(curDir, FileName)
		c, err := parseConfigFile(configPath)
		if err == nil {
			return c, nil
		}
		if os.IsNotExist(err) {
			curDir = filepath.Dir(curDir)
			continue
		}
		return nil, err
	}

	return nil, fmt.Errorf("config file not found")
}

func Init(webPkgName string) error {
	f, err := os.OpenFile(FileName, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	cfg := defaultConfig
	cfg.WebPackage = webPkgName

	return yaml.NewEncoder(f).Encode(cfg)
}

func parseConfigFile(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	config := defaultConfig
	if err := yaml.NewDecoder(f).Decode(&config); err != nil {
		return nil, err
	}
	config.Dir = filepath.Dir(path)
	return &config, nil
}
