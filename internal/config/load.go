package config

import (
	"fmt"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/structs"
	"github.com/knadh/koanf/v2"

	"github.com/HeoJeongBo/weft/internal/paths"
)

// Sources locates the config files to layer. Precedence, lowest to highest:
// defaults -> UserPath -> (OverridePath or ProjectPath). CLI flags are applied
// by the caller on top of the returned Config.
type Sources struct {
	UserPath     string // ~/.config/weft/config.yaml
	ProjectPath  string // <repo>/weft.yaml
	OverridePath string // --config; replaces project discovery when set
}

// loadDefaults seeds the koanf instance with built-in defaults. It is a seam so
// tests can exercise the (otherwise unreachable) error path.
var loadDefaults = func(k *koanf.Koanf) error {
	return k.Load(structs.Provider(Defaults(), "koanf"), nil)
}

// Load merges the configuration layers into a single Config.
func Load(src Sources) (Config, error) {
	k := koanf.New(".")

	if err := loadDefaults(k); err != nil {
		return Config{}, fmt.Errorf("load defaults: %w", err)
	}

	if src.UserPath != "" && paths.Exists(src.UserPath) {
		if err := k.Load(file.Provider(src.UserPath), yaml.Parser()); err != nil {
			return Config{}, fmt.Errorf("load %s: %w", src.UserPath, err)
		}
	}

	projectPath := src.ProjectPath
	if src.OverridePath != "" {
		projectPath = src.OverridePath
	}
	if projectPath != "" && paths.Exists(projectPath) {
		if err := k.Load(file.Provider(projectPath), yaml.Parser()); err != nil {
			return Config{}, fmt.Errorf("load %s: %w", projectPath, err)
		}
	}

	var c Config
	if err := k.Unmarshal("", &c); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	return c, nil
}
