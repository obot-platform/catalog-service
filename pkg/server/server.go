package server

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/go-github/v60/github"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
	_ "github.com/mattn/go-sqlite3"
	"github.com/sashabaranov/go-openai"
	"golang.org/x/oauth2"
)

var (
	db           *sql.DB
	githubClient *github.Client
	openaiClient *openai.Client
)

func Run() {
	// Load environment variables
	err := godotenv.Load()
	if err != nil {
		log.Println("Warning: Error loading .env file, using environment variables")
	}

	// Initialize database
	initDB()
	defer db.Close()

	// Initialize GitHub client
	initGitHubClient()

	// Initialize OpenAI client
	initOpenAIClient()

	startCronJobs()

	// Create API routes
	mux := http.NewServeMux()

	// Add CORS middleware
	corsMiddleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Set CORS headers
			w.Header().Set("Access-Control-Allow-Origin", "http://localhost:5175")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

			// Handle preflight requests
			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}

			// Call the next handler
			next.ServeHTTP(w, r)
		})
	}

	// Wrap your handlers with CORS middleware
	corsHandler := corsMiddleware(mux)

	mux.HandleFunc("GET /api/repos", getReposHandler)
	mux.HandleFunc("GET /api/repos/count", getReposCountHandler)
	mux.HandleFunc("GET /api/search", searchReposHandler)
	mux.HandleFunc("GET /api/search-readme", searchReposByReadmeHandler)
	mux.HandleFunc("GET /api/repos/{id}", getRepoHandler)
	mux.HandleFunc("PUT /api/repos/{id}", updateRepoHandler)
	mux.HandleFunc("PUT /api/repos/{id}/metadata", updateRepoMetadataHandler)
	mux.HandleFunc("POST /api/repos/{id}/generate", generateConfigForSpecificRepoHandler)
	mux.HandleFunc("POST /api/repos/{id}/run", runMCPServerHandler)

	// Create a file server for the static files
	fs := http.FileServer(http.Dir("./frontend/dist"))

	// Serve static files for all other routes
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Check if the requested file exists
		path := filepath.Join("./frontend/dist", r.URL.Path)
		_, err := os.Stat(path)

		// If the file doesn't exist, serve the index.html
		if os.IsNotExist(err) || r.URL.Path == "/" {
			http.ServeFile(w, r, "./frontend/dist/index.html")
			return
		}

		// Otherwise, let the file server handle it
		fs.ServeHTTP(w, r)
	})

	// Start server with CORS support
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("Server starting on port %s...", port)
	log.Fatal(http.ListenAndServe(":"+port, corsHandler))
}

func initDB() {
	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		log.Fatalf("POSTGRES_DSN environment variable is required")
	}
	// Add sslmode=disable to DSN if not already present
	if !strings.Contains(dsn, "sslmode=") {
		dsn += "?sslmode=disable"
	}

	var err error
	db, err = sql.Open("postgres", dsn)
	if err != nil {
		log.Fatalf("Error opening database: %v", err)
	}

	// Create repositories table
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS repositories (
			id SERIAL PRIMARY KEY,
			path TEXT,
			display_name TEXT,
			full_name TEXT UNIQUE,
			url TEXT,
			description TEXT,
			stars INTEGER,
			readme_content TEXT,
			language TEXT,
			manifest JSONB,
			icon TEXT,
			tool_definitions JSONB,
			metadata JSONB,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		log.Fatalf("Error creating repositories table: %v", err)
	}
}

func initGitHubClient() {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		log.Fatalf("GITHUB_TOKEN environment variable is required")
	}

	ctx := context.Background()
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)
	tc := oauth2.NewClient(ctx, ts)
	githubClient = github.NewClient(tc)
}

func initOpenAIClient() {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatalf("OPENAI_API_KEY environment variable is required")
	}
	openaiClient = openai.NewClient(apiKey)
}
