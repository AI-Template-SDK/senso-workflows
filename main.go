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
	"github.com/qdrant/go-client/qdrant"
	"github.com/typesense/typesense-go/v2/typesense"
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

	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)
	db.SetConnMaxLifetime(time.Duration(cfg.ConnMaxLifetime) * time.Second)

	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &database.Client{DB: db}, nil
}

func main() {
	if err := godotenv.Load(); err != nil {
		if err := godotenv.Load("dev.env"); err != nil {
			log.Printf("Note: No .env or dev.env file loaded: %v", err)
		} else {
			log.Printf("Loaded dev.env file for local development")
		}
	} else {
		log.Printf("Loaded .env file")
	}

	cfg := config.Load()

	log.Printf("Environment: %s", cfg.Environment)
	log.Printf("Port: %s", cfg.Port)
	log.Printf("Database Host: %s", cfg.Database.Host)
	log.Printf("Database Name: %s", cfg.Database.Name)

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

	ctx := context.Background()
	dbClient, err := createDatabaseClient(ctx, cfg.Database)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer dbClient.Close()
	log.Printf("Successfully connected to database")

	repoManager := services.NewRepositoryManager(dbClient)
	log.Printf("Repository manager initialized")

	if cfg.Environment == "development" || cfg.Environment == "" {
		os.Unsetenv("INNGEST_SIGNING_KEY")
		cfg.InngestSigningKey = ""
		log.Printf("Running in development mode - signing key verification disabled")
	}

	// === CORRECTED: INITIALIZE QDRANT AND TYPESENSE CLIENTS ===
	qdrantClient, err := qdrant.NewClient(&qdrant.Config{
		Host: cfg.Qdrant.Host,
		Port: cfg.Qdrant.Port,
	})
	if err != nil {
		log.Fatalf("Failed to create Qdrant client: %v", err)
	}
	log.Printf("Qdrant client initialized for host: %s", cfg.Qdrant.Host)

	typesenseClient := typesense.NewClient(
		typesense.WithServer(fmt.Sprintf("http://%s:%d", cfg.Typesense.Host, cfg.Typesense.Port)),
		typesense.WithAPIKey(cfg.Typesense.APIKey),
	)
	log.Printf("Typesense client initialized for host: %s", cfg.Typesense.Host)
	// === END CORRECTED ===

	// Initialize services with repository manager and proper dependencies
	orgService := services.NewOrgService(cfg, repoManager)
	dataExtractionService := services.NewDataExtractionService(cfg)
	questionRunnerService := services.NewQuestionRunnerService(cfg, repoManager, dataExtractionService)
	analyticsService := services.NewAnalyticsService(cfg, repoManager)
	
	// Corrected: Initialize OpenAI service correctly
	costService := services.NewCostService()
	openAIService := services.NewOpenAIProvider(cfg, "gpt-4-turbo", costService)

	// In sensor-workflows/main.go
	ingestionService := services.NewIngestionService(qdrantClient, typesenseClient, openAIService, repoManager, cfg)
	log.Printf("Ingestion service initialized")

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
	contentProcessor := workflows.NewContentProcessor(ingestionService)
	log.Printf("Content processor initialized")

	// Set client on workflows
	orgProcessor.SetClient(client)
	scheduledProcessor.SetClient(client)
	contentProcessor.SetClient(client)

	// Register functions
	orgProcessor.ProcessOrg()
	scheduledProcessor.DailyOrgProcessor()
	scheduledProcessor.WeeklyLoadAnalyzer()
	contentProcessor.ProcessWebsiteContent()

	// Create handler
	h := client.Serve()

	// Setup routes
	mux := http.NewServeMux()
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
	mux.HandleFunc("/test/trigger-org", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		testOrgID := "test-org-123"
		evt := inngestgo.Event{
			Name: "org.process",
			Data: map[string]interface{}{"org_id": testOrgID, "triggered_by": "manual_test", "user_id": "test-user"},
		}
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