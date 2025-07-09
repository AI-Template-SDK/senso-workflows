// internal/config/config.go
package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
)

// Added QdrantConfig struct
type QdrantConfig struct {
	Host string
	Port int
}

// Added TypesenseConfig struct
type TypesenseConfig struct {
	Host   string
	Port   int
	APIKey string
}

// Added SensoAPIConfig struct
type SensoAPIConfig struct {
	BaseURL string
	APIKey  string
}

type Config struct {
	Port              string
	Environment       string
	InngestEventKey   string
	InngestSigningKey string
	OpenAIAPIKey      string
	AnthropicAPIKey   string
	ApplicationAPIURL string
	DatabaseURL       string
	APIToken          string
	Database          DatabaseConfig
	Qdrant            QdrantConfig    // Added Qdrant config
	Typesense         TypesenseConfig // Added Typesense config
	SensoAPI          SensoAPIConfig  // Added SensoAPI config
}

// DatabaseConfig matches the senso-api database configuration structure exactly
type DatabaseConfig struct {
	Host            string
	Port            int
	User            string
	Password        string
	Name            string
	SSLMode         string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime int
}

func Load() *Config {
	config := &Config{
		Port:              getEnv("PORT", "8000"),
		Environment:       getEnv("ENVIRONMENT", "development"),
		InngestEventKey:   os.Getenv("INNGEST_EVENT_KEY"),
		InngestSigningKey: os.Getenv("INNGEST_SIGNING_KEY"),
		OpenAIAPIKey:      os.Getenv("OPENAI_API_KEY"),
		AnthropicAPIKey:   os.Getenv("ANTHROPIC_API_KEY"),
		ApplicationAPIURL: os.Getenv("APPLICATION_API_URL"),
		DatabaseURL:       os.Getenv("DATABASE_URL"),
		APIToken:          os.Getenv("API_TOKEN"),
	}

	// Parse database configuration
	dbConfig, err := parseDatabaseConfig()
	if err != nil {
		// If DATABASE_URL parsing fails, try individual env vars as fallback
		dbConfig = DatabaseConfig{
			Host:            getEnv("DB_HOST", "localhost"),
			Port:            getEnvInt("DB_PORT", 5432),
			User:            getEnv("DB_USER", "postgres"),
			Password:        getEnv("DB_PASSWORD", ""),
			Name:            getEnv("DB_NAME", "senso2"),
			SSLMode:         getEnv("DB_SSLMODE", "require"),
			MaxOpenConns:    getEnvInt("DB_MAX_OPEN_CONNS", 25),
			MaxIdleConns:    getEnvInt("DB_MAX_IDLE_CONNS", 25),
			ConnMaxLifetime: getEnvInt("DB_CONN_MAX_LIFETIME", 300),
		}
	}
	config.Database = dbConfig

	// === BLOCK TO LOAD NEW CONFIGS ===
	config.Qdrant = QdrantConfig{
		Host: getEnv("QDRANT_HOST", "qdrant"),
		Port: getEnvInt("QDRANT_PORT", 6334),
	}
	config.Typesense = TypesenseConfig{
		Host:   getEnv("TYPESENSE_HOST", "typesense"),
		Port:   getEnvInt("TYPESENSE_PORT", 8108),
		APIKey: getEnv("TYPESENSE_API_KEY", "xyz"),
	}
	config.SensoAPI = SensoAPIConfig{
		// 'host.docker.internal' lets this container talk to a service exposed on your local machine (the Mac)
		BaseURL: getEnv("SENSO_API_URL", "http://host.docker.internal:8000"),
		APIKey:  getEnv("SENSO_API_KEY", "tgr_test_key_for_development_only"),
	}
	// === END BLOCK ===

	return config
}

func parseDatabaseConfig() (DatabaseConfig, error) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return DatabaseConfig{}, fmt.Errorf("DATABASE_URL not set")
	}

	parsedURL, err := url.Parse(dbURL)
	if err != nil {
		return DatabaseConfig{}, fmt.Errorf("invalid DATABASE_URL: %w", err)
	}

	config := DatabaseConfig{
		Host:            parsedURL.Hostname(),
		Port:            5432, // default
		User:            parsedURL.User.Username(),
		Name:            parsedURL.Path[1:], // remove leading slash
		SSLMode:         getEnv("DB_SSLMODE", "require"),
		MaxOpenConns:    getEnvInt("DB_MAX_OPEN_CONNS", 25),
		MaxIdleConns:    getEnvInt("DB_MAX_IDLE_CONNS", 25),
		ConnMaxLifetime: getEnvInt("DB_CONN_MAX_LIFETIME", 300),
	}

	if password, ok := parsedURL.User.Password(); ok {
		config.Password = password
	}

	if parsedURL.Port() != "" {
		if port, err := strconv.Atoi(parsedURL.Port()); err == nil {
			config.Port = port
		}
	}

	return config, nil
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intVal, err := strconv.Atoi(value); err == nil {
			return intVal
		}
	}
	return defaultValue
}