package main

import (
	"context"
	"os"
	"strings"
	"text/template"
	"time"

	"github.com/heetch/confita"
	"github.com/heetch/confita/backend"
	"github.com/heetch/confita/backend/file"
	"github.com/prometheus/common/log"
)

type Duration time.Duration

func (d *Duration) UnmarshalJSON(b []byte) error {
	dur, err := time.ParseDuration(string(b))

	if err != nil {
		return err
	}

	*d = Duration(dur)

	return nil
}

func (d *Duration) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var str string
	err := unmarshal(&str)

	if err != nil {
		return err
	}

	dur, err := time.ParseDuration(str)

	if err != nil {
		return err
	}

	*d = Duration(dur)
	return nil
}

type Config struct {
	CalendarEndpoints []string `json:"calendar_endpoint" yaml:"calendar_endpoint" config:"calendar_endpoint"`
	CheckInterval     Duration `json:"check_interval" yaml:"check_interval" config:"check_interval"`
	DBPath            string   `json:"db_path" yaml:"db_path" config:"db_path"`
	Discord           struct {
		Token          string   `json:"token" yaml:"token" config:"discord_token"`
		GuildID        string   `json:"guild_id" yaml:"guild_id" config:"discord_guild_id"`
		Parent         string   `json:"parent" yaml:"parent" config:"discord_parent"`
		Parents        []string `json:"parents" yaml:"parents" config:"discord_parents"`
		DefaultChannel string   `json:"default_channel" yaml:"default_channel" config:"discord_default_channel"`
	} `json:"discord" yaml:"discord"`
	Notification struct {
		Templtes        map[string]string             `json:"templates" yaml:"templates" config:"-"`
		ParsedTemplates map[string]*template.Template `json:"-" yaml:"-" config:"-"`
		Schedules       []Duration                    `json:"schedules" yaml:"schedules" config:"-"`
	} `json:"notification" yaml:"notification"`
}

// NewBackend creates a configuration loader that loads from the environment.
// If the key is not found, this backend tries again by turning any kebabcase key to snakecase and
// lowercase letters to uppercase.
func NewBackend() backend.Backend {
	return backend.Func("env", func(ctx context.Context, key string) ([]byte, error) {
		if val := os.Getenv(key); val != "" {
			return []byte(val), nil
		}
		key = strings.Replace(strings.ToUpper(key), "-", "_", -1)
		if val := os.Getenv(key); val != "" {
			return []byte(val), nil
		}
		return nil, backend.ErrNotFound
	})
}

func LoadConfig() *Config {
	bs := []backend.Backend{
		NewBackend(),
		file.NewOptionalBackend("./ical-bot.yaml"),
	}

	cfg := &Config{}
	if err := confita.NewLoader(bs...).Load(context.Background(), cfg); err != nil {
		panic(err)
	}

	funcs := template.FuncMap{
		"EncodeTimestamp": encodeTimestamp,
		"EncodeDuration":  encodeDuration,
	}

	cfg.Notification.ParsedTemplates = map[string]*template.Template{}
	for key, val := range cfg.Notification.Templtes {
		tmpl, err := template.New(key).Funcs(funcs).Parse(val)

		if err != nil {
			log.Fatalf("failed to parse template for %s: %+v", key, err)
		}

		cfg.Notification.ParsedTemplates[key] = tmpl
	}

	return cfg
}
