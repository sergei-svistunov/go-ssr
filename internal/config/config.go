package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const FileName = "gossr.yaml"

var defaultConfig = Config{
	WebDir:    "./internal/web",
	GoRunArgs: ".",
}

type Config struct {
	Dir         string            `yaml:"-"`
	Prod        bool              `yaml:"-"`
	WebDir      string            `yaml:"webDir"`      // Directory containing SSR handlers and templates
	WebPackage  string            `yaml:"webPackage"`  // Full path to the web package
	DepsPackage string            `yaml:"depsPackage"` // Full path to the deps package containing Deps struct
	DepsType    string            `yaml:"depsType"`    // Type name in the deps package (default: "Deps")
	GoRunArgs   string            `yaml:"goRunArgs"`   // Arguments for `go run`
	Env         map[string]string `yaml:"env"`         // Environment variables
}

// Read finds the project config by walking up from the working directory.
func Read() (*Config, error) {
	curDir, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	return ReadFrom(curDir)
}

// ReadFrom finds the project config by walking up from startDir. It lets a
// caller that is not running inside the project — a server told which project to
// operate on, for example — resolve the config the same way the CLI does.
func ReadFrom(startDir string) (*Config, error) {
	curDir, err := filepath.Abs(startDir)
	if err != nil {
		return nil, err
	}

	for {
		configPath := filepath.Join(curDir, FileName)
		c, err := parseConfigFile(configPath)
		if err == nil {
			return c, nil
		}
		if !os.IsNotExist(err) {
			return nil, err
		}

		parent := filepath.Dir(curDir)
		if parent == curDir {
			return nil, fmt.Errorf("%s not found in %s or any parent directory", FileName, startDir)
		}
		curDir = parent
	}
}

// InitAt writes a config file for a new project in dir, filling in defaults for
// the fields the caller left empty. It refuses to overwrite an existing file.
func InitAt(dir string, cfg Config) error {
	if cfg.WebDir == "" {
		cfg.WebDir = defaultConfig.WebDir
	}
	if cfg.GoRunArgs == "" {
		cfg.GoRunArgs = defaultConfig.GoRunArgs
	}
	if cfg.DepsPackage != "" && cfg.DepsType == "" {
		cfg.DepsType = "Deps"
	}

	f, err := os.OpenFile(filepath.Join(dir, FileName), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

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
	// webDir is written relative to the config file, not to the process working
	// directory, so go-ssr behaves the same whether it is run from the project
	// root or from a subdirectory (an MCP host or an IDE picks the working
	// directory, not the user).
	if !filepath.IsAbs(config.WebDir) {
		config.WebDir = filepath.Join(config.Dir, config.WebDir)
	}
	if config.DepsPackage != "" && config.DepsType == "" {
		config.DepsType = "Deps"
	}
	return &config, nil
}
