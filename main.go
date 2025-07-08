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
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

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
	// ---- START: LOCAL DOCKER-COMPOSE TEST BLOCK ----
	// if os.Getenv("LOCAL_TEST_MODE") == "true" {
	// 	log.Println("--- RUNNING IN LOCAL TEST MODE ---")

	// 	// Test Qdrant Connection
	// 	qdrantHost := os.Getenv("QDRANT_HOST")
	// 	qdrantPort := os.Getenv("QDRANT_PORT")
	// 	qdrantAddr := fmt.Sprintf("%s:%s", qdrantHost, qdrantPort)

	// 	qdrantConn, err := grpc.Dial(qdrantAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	// 	if err != nil {
	// 		log.Fatalf("Qdrant connection failed: %v", err)
	// 	}
	// 	defer qdrantConn.Close()

	// 	// Instead of a health client, we create a collections client to test the connection.
	// 	collectionsClient := qdrant.NewCollectionsClient(qdrantConn)

	// 	// We test the connection by trying to list the collections. An empty response is a success.
	// 	_, err = collectionsClient.List(context.Background(), &qdrant.ListCollectionsRequest{})
	// 	if err != nil {
	// 		log.Printf("❌ Qdrant list collections check failed: %v\n", err)
	// 	} else {
	// 		log.Println("✅ Successfully connected to local Qdrant!")
	// 	}

	// 	// Test Typesense Connection
	// 	typesenseHost := os.Getenv("TYPESENSE_HOST")
	// 	typesensePort := os.Getenv("TYPESENSE_PORT")
	// 	typesenseAddr := fmt.Sprintf("http://%s:%s", typesenseHost, typesensePort)
	// 	typesenseAPIKey := os.Getenv("TYPESENSE_API_KEY")

	// 	req, _ := http.NewRequest("GET", typesenseAddr+"/health", nil)
	// 	req.Header.Set("X-TYPESENSE-API-KEY", typesenseAPIKey)

	// 	resp, err := http.DefaultClient.Do(req)
	// 	if err != nil {
	// 		log.Printf("❌ Typesense health check failed: %v\n", err)
	// 	} else if resp.StatusCode != http.StatusOK {
	// 		log.Printf("❌ Typesense health check returned non-200 status: %s", resp.Status)
	// 	} else {
	// 		log.Println("✅ Successfully connected to local Typesense!")
	// 	}
	// 	defer resp.Body.Close()

	// 	log.Println("--- LOCAL TEST MODE FINISHED, IDLING ---")
	// 	select {}
	// }
	// ---- END: LOCAL DOCKER-COMPOSE TEST BLOCK ----

	// Load environment variables from .env file first (standard practice)
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

	// === NEW: INITIALIZE QDRANT AND TYPESENSE CLIENTS ===
	qdrantAddr := fmt.Sprintf("%s:%d", cfg.Qdrant.Host, cfg.Qdrant.Port)
	qdrantConn, err := grpc.Dial(qdrantAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("Failed to create Qdrant connection: %v", err)
	}
	qdrantClient := qdrant.NewClient(qdrantConn)
	log.Printf("Qdrant client initialized for host: %s", qdrantAddr)

	typesenseClient := typesense.NewClient(
		typesense.WithServer(fmt.Sprintf("http://%s:%d", cfg.Typesense.Host, cfg.Typesense.Port)),
		typesense.WithAPIKey(cfg.Typesense.APIKey),
	)
	log.Printf("Typesense client initialized for host: %s", cfg.Typesense.Host)
	// === END NEW ===

	// Initialize services with repository manager and proper dependencies
	orgService := services.NewOrgService(cfg, repoManager)
	dataExtractionService := services.NewDataExtractionService(cfg)
	questionRunnerService := services.NewQuestionRunnerService(cfg, repoManager, dataExtractionService)
	analyticsService := services.NewAnalyticsService(cfg, repoManager)

	// === NEW: INITIALIZE INGESTION SERVICE ===
	openAIService := services.NewOpenAIService(cfg)
	ingestionService := services.NewIngestionService(qdrantClient, typesenseClient, openAIService, cfg)
	log.Printf("Ingestion service initialized")
	// === END NEW ===

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

	// === NEW: INITIALIZE CONTENT PROCESSOR ===
	contentProcessor := workflows.NewContentProcessor(ingestionService)
	log.Printf("Content processor initialized")
	// === END NEW ===

	// Set client on workflows
	orgProcessor.SetClient(client)
	scheduledProcessor.SetClient(client)
	// === NEW: SET CLIENT ON CONTENT PROCESSOR ===
	contentProcessor.SetClient(client)
	// === END NEW ===

	// Register functions (they auto-register with the client when created)
	orgProcessor.ProcessOrg()
	scheduledProcessor.DailyOrgProcessor()
	scheduledProcessor.WeeklyLoadAnalyzer()
	// === NEW: REGISTER CONTENT WORKFLOW FUNCTION ===
	contentProcessor.ProcessWebsiteContent()
	// === END NEW ===

	// Create handler
	h := client.Serve()

	// Setup routes
	mux := http.NewServeMux()

	// Inngest webhook endpoint
	mux.Handle("/api/inngest", h)

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
			Data: map[string]interface{}{"org_id": testOrgID, "triggered_by": "manual_test", "user_id": "test-user"},
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