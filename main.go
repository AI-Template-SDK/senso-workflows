// main.go
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/inngest/inngestgo"
	"github.com/jmoiron/sqlx"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"

	"github.com/AI-Template-SDK/senso-api/pkg/database"
	"github.com/AI-Template-SDK/senso-workflows/internal/config"
	"github.com/AI-Template-SDK/senso-workflows/services"
	"github.com/AI-Template-SDK/senso-workflows/workflows"
)

// createDatabaseClient creates a database client using our config structure
func createDatabaseClient(ctx context.Context, cfg config.DatabaseConfig) (*database.Client, error) {
	connStr := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		cfg.Host, cfg.Port, cfg.User, cfg.Password, cfg.Name, cfg.SSLMode,
	)

	db, err := sqlx.ConnectContext(ctx, "postgres", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)
	db.SetConnMaxLifetime(time.Duration(cfg.ConnMaxLifetime) * time.Second)

	// Test connection
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &database.Client{DB: db}, nil
}

func main() {
	// Load environment variables from .env file first (standard practice)
	// If not found, try dev.env for local development
	if err := godotenv.Load(); err != nil {
		// Try dev.env as fallback for local development
		if err := godotenv.Load("dev.env"); err != nil {
			// It's OK if neither file exists, we'll use environment variables
			log.Printf("Note: No .env or dev.env file loaded: %v", err)
		} else {
			log.Printf("Loaded dev.env file for local development")
		}
	} else {
		log.Printf("Loaded .env file")
	}

	cfg := config.Load()

	// Log environment for debugging
	log.Printf("Environment: %s", cfg.Environment)
	log.Printf("Port: %s", cfg.Port)
	log.Printf("Database Host: %s", cfg.Database.Host)
	log.Printf("Database Name: %s", cfg.Database.Name)

	// Log API key status (without exposing the actual keys)
	if cfg.OpenAIAPIKey == "" {
		log.Printf("WARNING: OpenAI API key not loaded!")
	} else {
		log.Printf("OpenAI API key loaded (length: %d)", len(cfg.OpenAIAPIKey))
	}
	if cfg.AnthropicAPIKey == "" {
		log.Printf("WARNING: Anthropic API key not loaded!")
	} else {
		log.Printf("Anthropic API key loaded (length: %d)", len(cfg.AnthropicAPIKey))
	}

	// Initialize database connection using our custom function
	ctx := context.Background()
	dbClient, err := createDatabaseClient(ctx, cfg.Database)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer dbClient.Close()
	log.Printf("Successfully connected to database")

	// Create repository manager
	repoManager := services.NewRepositoryManager(dbClient)
	log.Printf("Repository manager initialized")

	// In development, we don't need signing keys with the local dev server
	if cfg.Environment == "development" || cfg.Environment == "" {
		// Clear the signing key for local development
		os.Unsetenv("INNGEST_SIGNING_KEY")
		cfg.InngestSigningKey = ""
		log.Printf("Running in development mode - signing key verification disabled")
	}

	// Initialize services with repository manager and proper dependencies
	orgService := services.NewOrgService(cfg, repoManager)
	dataExtractionService := services.NewDataExtractionService(cfg)
	questionRunnerService := services.NewQuestionRunnerService(cfg, repoManager, dataExtractionService)
	analyticsService := services.NewAnalyticsService(cfg, repoManager)

	// Create Inngest client
	client, err := inngestgo.NewClient(
		inngestgo.ClientOpts{
			AppID:    "senso-workflows",
			EventKey: inngestgo.StrPtr(cfg.InngestEventKey),
			Env:      inngestgo.StrPtr(cfg.Environment),
		},
	)
	if err != nil {
		log.Fatalf("Failed to create Inngest client: %v", err)
	}

	// Initialize workflows with services
	orgProcessor := workflows.NewOrgProcessor(
		orgService,
		analyticsService,
		questionRunnerService,
		cfg,
	)
	scheduledProcessor := workflows.NewScheduledProcessor(orgService)

	// Set client on workflows
	orgProcessor.SetClient(client)
	scheduledProcessor.SetClient(client)

	// Register functions (they auto-register with the client when created)
	orgProcessor.ProcessOrg()
	scheduledProcessor.DailyOrgProcessor()
	scheduledProcessor.WeeklyLoadAnalyzer()

	// Create handler
	h := client.Serve()

	// Setup routes
	mux := http.NewServeMux()

	// Inngest webhook endpoint
	mux.Handle("/api/inngest", h)

	// Root endpoint for ALB health check
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"service":"senso-workflows","status":"running"}`))
	})

	// Health check
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"healthy"}`))
	})

	// Test endpoint to trigger ProcessOrg workflow
	mux.HandleFunc("/test/trigger-org", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		// Create test event
		testOrgID := "test-org-123"
		evt := inngestgo.Event{
			Name: "org.process",
			Data: map[string]interface{}{
				"org_id":       testOrgID,
				"triggered_by": "manual_test",
				"user_id":      "test-user",
			},
		}

		// Send event
		result, err := client.Send(r.Context(), evt)
		if err != nil {
			log.Printf("Failed to send test event: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(fmt.Sprintf(`{"error":"Failed to send event: %v"}`, err)))
			return
		}

		log.Printf("Test event sent successfully: %+v", result)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(fmt.Sprintf(`{"status":"success","message":"Test event sent for org %s","event_ids":["%s"]}`, testOrgID, result)))
	})

	// Start server
	port := cfg.Port
	log.Printf("Starting Senso Workflows service on port %s", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatal(err)
	}
}
