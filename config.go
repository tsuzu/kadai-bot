package main

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/heetch/confita"
	"github.com/heetch/confita/backend"
	"github.com/heetch/confita/backend/file"
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
	CalendarEndpoint     string     `json:"calendar_endpoint" yaml:"calendar_endpoint" config:"calendar_endpoint"`
	CheckInterval        Duration   `json:"check_interval" yaml:"check_interval" config:"check_interval"`
	DBPath               string     `json:"db_path" yaml:"db_path" config:"db_path"`
	NotificationSchedule []Duration `json:"notification" yaml:"notification" config:"-"`
	DiscordToken         string     `json:"discord_token" yaml:"discord_token" config:"discord_token"`
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

	return cfg
}
