// internal/config/config.go
package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
)

type Config struct {
	Port                      string
	Environment               string
	InngestEventKey           string
	InngestSigningKey         string
	OpenAIAPIKey              string
	AnthropicAPIKey           string
	AzureOpenAIEndpoint       string
	AzureOpenAIKey            string
	AzureOpenAIDeploymentName string
	ApplicationAPIURL         string
	DatabaseURL               string
	APIToken                  string
	BrightDataAPIKey          string
	BrightDataDatasetID       string
	PerplexityDatasetID       string
	GeminiDatasetID           string
	LinkupAPIKey              string
	OxylabsUsername           string
	OxylabsPassword           string
	ScrapingBeeAPIKey         string
	ScrapelessAPIKey          string
	EnableScheduledPipelines  bool
	Database                  DatabaseConfig
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
		Port:                      getEnv("PORT", "8000"),
		Environment:               getEnv("ENVIRONMENT", "development"),
		InngestEventKey:           os.Getenv("INNGEST_EVENT_KEY"),
		InngestSigningKey:         os.Getenv("INNGEST_SIGNING_KEY"),
		OpenAIAPIKey:              os.Getenv("OPENAI_API_KEY"),
		AnthropicAPIKey:           os.Getenv("ANTHROPIC_API_KEY"),
		AzureOpenAIEndpoint:       os.Getenv("AZURE_OPENAI_ENDPOINT"),
		AzureOpenAIKey:            os.Getenv("AZURE_OPENAI_KEY"),
		AzureOpenAIDeploymentName: os.Getenv("AZURE_OPENAI_DEPLOYMENT_NAME"),
		ApplicationAPIURL:         os.Getenv("APPLICATION_API_URL"),
		DatabaseURL:               os.Getenv("DATABASE_URL"),
		APIToken:                  os.Getenv("API_TOKEN"),
		BrightDataAPIKey:          os.Getenv("BRIGHTDATA_API_KEY"),
		BrightDataDatasetID:       os.Getenv("BRIGHTDATA_DATASET_ID"),
		PerplexityDatasetID:       os.Getenv("PERPLEXITY_DATASET_ID"),
		GeminiDatasetID:           os.Getenv("GEMINI_DATASET_ID"),
		LinkupAPIKey:              os.Getenv("LINKUP_API_KEY"),
		OxylabsUsername:           os.Getenv("OXYLABS_USERNAME"),
		OxylabsPassword:           os.Getenv("OXYLABS_PASSWORD"),
		ScrapingBeeAPIKey:         os.Getenv("SCRAPINGBEE_API_KEY"),
		ScrapelessAPIKey:          os.Getenv("SCRAPELESS_API_KEY"),
		EnableScheduledPipelines:  getEnvBool("ENABLE_SCHEDULED_PIPELINES", true),
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

func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		switch value {
		case "1", "true", "TRUE", "True", "yes", "YES", "Yes", "on", "ON", "On":
			return true
		case "0", "false", "FALSE", "False", "no", "NO", "No", "off", "OFF", "Off":
			return false
		}
	}
	return defaultValue
}
