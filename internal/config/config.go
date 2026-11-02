package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	Addr       string
	DSN        string
	Migrations string

	MinDuration time.Duration
	MaxDuration time.Duration
	Shutdown    time.Duration

	Retries int
}

func get(k string, d string) string {
	v := os.Getenv(k)
	if v != "" {
		return v
	}
	return d
}

func dur(k string, d string) time.Duration {
	v, e := time.ParseDuration(get(k, d))
	if e != nil {
		x, _ := time.ParseDuration(d)
		return x
	}
	return v
}

func num(k string, d int) int {
	v, e := strconv.Atoi(get(k, strconv.Itoa(d)))
	if e != nil {
		return d
	}
	return v
}

func Load() Config {
	return Config{
		get("HTTP_ADDR", ":8080"),
		get("DATABASE_URL", "postgres://schedule:local_dev_only@localhost:5432/schedule?sslmode=disable"),
		get("MIGRATIONS_DIR", "migrations"),
		dur("MIN_BOOKING_DURATION", "15m"),
		dur("MAX_BOOKING_DURATION", "8h"),
		dur("SHUTDOWN_TIMEOUT", "10s"),
		num("SERIALIZABLE_RETRIES", 3),
	}
}
