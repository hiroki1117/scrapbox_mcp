package config

import (
	"time"

	"github.com/caarlos0/env/v10"
)

type Config struct {
	// Server configuration
	Port        string `env:"PORT" envDefault:"8080"`
	Environment string `env:"ENVIRONMENT" envDefault:"production"`
	LogLevel    string `env:"LOG_LEVEL" envDefault:"info"`

	// MCP configuration
	SessionTTL time.Duration `env:"SESSION_TTL" envDefault:"1h"`
	EnableSSE  bool          `env:"ENABLE_SSE" envDefault:"true"`

	// Scrapbox configuration
	ProjectName   string `env:"COSENSE_PROJECT_NAME,required"`
	SessionCookie string `env:"COSENSE_SID,required"`

	// API configuration
	RestAPIBaseURL string        `env:"SCRAPBOX_API_URL" envDefault:"https://scrapbox.io/api"`
	WebSocketURL   string        `env:"SCRAPBOX_WS_URL" envDefault:"wss://scrapbox.io/socket.io/"`
	RequestTimeout time.Duration `env:"REQUEST_TIMEOUT" envDefault:"30s"`
	MaxRetries     int           `env:"MAX_RETRIES" envDefault:"3"`

	// Security
	AllowedOrigins []string `env:"ALLOWED_ORIGINS" envSeparator:","`
	EnableCORS     bool     `env:"ENABLE_CORS" envDefault:"true"`
}

func Load() (*Config, error) {
	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}
