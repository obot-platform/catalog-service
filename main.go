package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/go-github/v60/github"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	_ "github.com/mattn/go-sqlite3"
	"github.com/robfig/cron/v3"
	"github.com/sashabaranov/go-openai"
	"golang.org/x/oauth2"
)

// RepoInfo stores information about a repository
type RepoInfo struct {
	ID              int    `json:"id"`
	Path            string `json:"path"`
	DisplayName     string `json:"displayName"`
	FullName        string `json:"fullName"`
	URL             string `json:"url"`
	Description     string `json:"description"`
	Stars           int    `json:"stars"`
	ReadmeContent   string `json:"readmeContent"`
	Language        string `json:"language"`
	Metadata        string `json:"metadata"`
	License         string `json:"license"`
	Icon            string `json:"icon"`
	Manifest        string `json:"manifest"`
	ToolDefinitions string `json:"toolDefinitions"`
}

type MCPServerManifest struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Category    string            `json:"category"`
	Configs     []MCPServerConfig `json:"configs"`
}

type Config struct {
	MCPServers map[string]MCPServerConfig `json:"mcpServers"`
}

type MCPServerConfig struct {
	Env         []MCPPair `json:"env"`
	Command     string    `json:"command,omitempty"`
	Args        []string  `json:"args,omitempty"`
	HTTPHeaders []MCPPair `json:"httpHeaders,omitempty"`
	URL         string    `json:"url,omitempty"`
}

type MCPPair struct {
	Key         string `json:"key,omitempty"`
	Value       string `json:"value,omitempty"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
	Sensitive   bool   `json:"sensitive"`
}

// MCPTool represents a tool provided by an MCP server
type MCPTool struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	InputSchema json.RawMessage   `json:"input_schema,omitempty"`
	Auth        map[string]string `json:"auth,omitempty"`
}

var (
	db           *sql.DB
	githubClient *github.Client
	openaiClient *openai.Client
)

func main() {
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

func collectData() {
	ctx := context.Background()
	log.Println("Searching repositories by README content...")
	limit, _ := strconv.Atoi(os.Getenv("LIMIT"))
	if limit == 0 {
		limit = 4000
	}
	searchReposByReadme(ctx, limit)
}

func analyzeWithOpenAI(repoName, readmeContent, existingConfig string) (MCPServerManifest, error) {
	var result MCPServerManifest

	// Create the prompt
	prompt := fmt.Sprintf(`
You are an expert in Model Context Protocol (MCP) servers. Analyze the following README from the repository %s:

%s

Extract and provide the following data structure in JSON format:

type OpenAIResponse struct {
	Configs     []MCPServerConfig json:"configs"
	Name        string            json:"name"
	Description string            json:"description"
	Category    string            json:"category"
}

type MCPServerConfig struct {
	Env         []MCPPair json:"env"
	Command     string    json:"command,omitempty"
	Args        []string json:"args,omitempty"
	HTTPHeaders []MCPPair json:"httpHeaders,omitempty"
	URL         string    json:"url,omitempty"
}

type MCPPair struct {
	Key         string json:"key,omitempty"
	Value       string json:"value,omitempty"
	Name        string json:"name"
	Description string json:"description"
	Required    bool   json:"required"
	Sensitive   bool   json:"sensitive"
}

If the repository does not contain an MCP server, respond with an empty JSON object.

For MCPServerConfig, you should look for a MCP server config in readme that looks like this:

"mcpServers": {
  ...
}

When generating category, pick from the following categories:

- Popular
- Featured
- Cloud Platforms
- Security & Compliance
- Developer Tools
- TypeScript
- Python
- Go
- Art & Culture
- Analytics & Data
- E-commerce
- Marketing & Social Media
- Productivity
- Education

It can have multiple categories. connect them with comma.

You should also look for mcp server config whose command is only npx, docker and uv. If command is not one of these, you should not return empty json object.

Make sure you can extract command, args and env from the mcp config example in the readme.
It is usually wrapped into json block. For other MCPPair, you should look in the readme to find possible explaination.

Return OpenAIResponse which contains a list of MCPServerManifest which supports docker, npx and uv and a category.

You have existing config generated by AI previous for reference: %s
`, repoName, readmeContent, existingConfig)

	// Call OpenAI API
	resp, err := openaiClient.CreateChatCompletion(
		context.Background(),
		openai.ChatCompletionRequest{
			Model: openai.GPT4o,
			Messages: []openai.ChatCompletionMessage{
				{
					Role:    openai.ChatMessageRoleUser,
					Content: prompt,
				},
			},
			ResponseFormat: &openai.ChatCompletionResponseFormat{
				Type: openai.ChatCompletionResponseFormatTypeJSONObject,
			},
		},
	)

	if err != nil {
		return result, fmt.Errorf("OpenAI API error: %v", err)
	}

	if len(resp.Choices) == 0 {
		return result, fmt.Errorf("no response from OpenAI")
	}

	// Parse the JSON response
	err = json.Unmarshal([]byte(resp.Choices[0].Message.Content), &result)
	if err != nil {
		return result, fmt.Errorf("error parsing OpenAI response: %v", err)
	}

	return result, nil
}

func saveRepo(repo RepoInfo) {
	// Check if repository already exists
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM repositories WHERE full_name = $1", repo.FullName).Scan(&count)
	if err != nil {
		log.Printf("Error checking if repository exists: %v", err)
		return
	}

	if count > 0 {
		// Update existing repository
		_, err = db.Exec(`
			UPDATE repositories 
			SET url = $1, description = $2, display_name = $3, stars = $4, readme_content = $5, 
				language = $6, path = $7, manifest = $8::jsonb, icon = $9, metadata = $10::jsonb
			WHERE full_name = $11
		`, repo.URL, repo.Description, repo.DisplayName, repo.Stars, repo.ReadmeContent,
			repo.Language, repo.Path, repo.Manifest, repo.Icon, repo.Metadata, repo.FullName)
		if err != nil {
			log.Printf("Error updating repository %s: %v", repo.FullName, err)
		} else {
			log.Printf("Updated repository %s", repo.FullName)
		}
	} else {
		// Insert new repository
		if repo.Metadata == "" {
			repo.Metadata = "{}"
		}
		_, err = db.Exec(`
			INSERT INTO repositories 
			(full_name, url, description, display_name, stars, readme_content, language, path, manifest, icon, metadata) 
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		`, repo.FullName, repo.URL, repo.Description, repo.DisplayName, repo.Stars, repo.ReadmeContent,
			repo.Language, repo.Path, []byte(repo.Manifest), repo.Icon, []byte(repo.Metadata))
		if err != nil {
			log.Printf("Error inserting repository %s: %v", repo.FullName, err)
		} else {
			log.Printf("Inserted repository %s", repo.FullName)
		}
	}
}

func searchReposByReadme(ctx context.Context, limit int) {
	opts := &github.SearchOptions{
		ListOptions: github.ListOptions{
			PerPage: 1000,
		},
	}
	var allRepos []*github.CodeResult

	// List of repos to check
	reposToCheck := []string{
		"modelcontextprotocol/servers",
		"awslabs/mcp",
		"punkpeye/awesome-mcp-servers",
	}

	// First get all repo links from these repos' READMEs
	var repoLinks []string
	for _, repoFullName := range reposToCheck {
		parts := strings.Split(repoFullName, "/")
		owner, repo := parts[0], parts[1]

		// Get README content
		fileContent, _, _, err := githubClient.Repositories.GetContents(
			ctx,
			owner,
			repo,
			"README.md",
			nil,
		)
		if err != nil {
			log.Printf("Error getting README for %s: %v", repoFullName, err)
			continue
		}

		content, err := fileContent.GetContent()
		if err != nil {
			log.Printf("Error decoding README content for %s: %v", repoFullName, err)
			continue
		}

		// Extract GitHub repo links using simple regex
		matches := regexp.MustCompile(`github\.com/([^\s/()]+/[^\s/()]+)`).FindAllStringSubmatch(content, -1)
		for _, match := range matches {
			if len(match) > 1 {
				repoLinks = append(repoLinks, match[1])
			}
		}
	}
	log.Printf("Found %d repos to check", len(repoLinks))

	// Now search for mcpServers in README of each repo found
	// Process repos in batches of 30
	batchSize := 15
	for i := 0; i < len(repoLinks); i += batchSize {
		end := i + batchSize
		if end > len(repoLinks) {
			end = len(repoLinks)
		}

		var queryParts []string
		for _, repoFullName := range repoLinks[i:end] {
			queryParts = append(queryParts, fmt.Sprintf("repo:%s", repoFullName))
		}
		query := fmt.Sprintf("%s mcpServers filename:README.md", strings.Join(queryParts, " "))

		result, resp, err := githubClient.Search.Code(ctx, query, opts)
		if err != nil {
			if _, ok := err.(*github.RateLimitError); ok {
				log.Printf("Hit rate limit, waiting for reset after time %s...\n", time.Until(resp.Rate.Reset.Time))
				time.Sleep(time.Until(resp.Rate.Reset.Time))
				continue
			}
			log.Printf("Error searching repositories: %v", err)
			continue
		}
		log.Printf("Found %d repos in batch %d", len(result.CodeResults), i/batchSize+1)

		allRepos = append(allRepos, result.CodeResults...)
		if len(allRepos) >= limit {
			break
		}
		time.Sleep(time.Second * 5)
	}

	// Search for repositories with "mcpServers" in their README files
	query := "mcpServers filename:README.md"

	for {
		if len(allRepos) >= limit {
			break
		}
		result, resp, err := githubClient.Search.Code(ctx, query, opts)
		if err != nil {
			// Handle rate limiting
			if _, ok := err.(*github.RateLimitError); ok {
				log.Printf("Hit rate limit, waiting for reset after time %s...\n", time.Until(resp.Rate.Reset.Time))
				time.Sleep(time.Until(resp.Rate.Reset.Time))
				continue
			}
			log.Printf("Error searching repositories: %v", err)
			return
		}

		log.Printf("Found %d repositories", len(result.CodeResults))
		allRepos = append(allRepos, result.CodeResults...)

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
		time.Sleep(5 * time.Second)
	}

	// Deduplicate repositories based on fullname and path
	seen := make(map[string]bool)
	uniqueRepos := make([]*github.CodeResult, 0)

	for _, repo := range allRepos {
		// Create unique key from fullname and path
		key := *repo.Repository.FullName + ":" + *repo.Path
		if !seen[key] {
			seen[key] = true
			uniqueRepos = append(uniqueRepos, repo)
		}
	}
	allRepos = uniqueRepos

	log.Printf("Found %d unique repositories", len(allRepos))

	// Process and store the repositories
	for _, repo := range allRepos {
		log.Printf("Processing repository: %s", *repo.Repository.FullName)
		githubRepo, _, err := githubClient.Repositories.Get(ctx, *repo.Repository.Owner.Login, *repo.Repository.Name)
		if err != nil {
			log.Printf("Error getting repository %s: %v", *repo.Repository.FullName, err)
			continue
		}

		// Get README content from the specific path where it was found
		readmeContent := ""
		fileContent, _, _, err := githubClient.Repositories.GetContents(
			ctx,
			*githubRepo.Owner.Login,
			*githubRepo.Name,
			*repo.Path,
			nil,
		)
		if err != nil {
			log.Printf("Error getting README for %s: %v", *repo.Repository.FullName, err)
		} else {
			readmeContent, err = fileContent.GetContent()
			if err != nil {
				log.Printf("Error decoding README for %s: %v", *repo.Repository.FullName, err)
			}
		}

		fullName := *githubRepo.FullName
		parts := strings.Split(repo.GetPath(), "/")
		if len(parts) > 1 {
			// Join all parts except the last one and append to fullName
			fullName = fullName + "/" + strings.Join(parts[:len(parts)-1], "/")
		}

		// Construct URL with correct path
		repoURL := githubRepo.GetHTMLURL()
		if len(parts) > 1 {
			// Add path components to URL, excluding the filename
			repoURL = repoURL + "/tree/" + githubRepo.GetDefaultBranch() + "/" + strings.Join(parts[:len(parts)-1], "/")
		}

		if !strings.Contains(readmeContent, "mcpServers") && !strings.Contains(readmeContent, "npx") && !strings.Contains(readmeContent, "docker") && !strings.Contains(readmeContent, "uv") {
			continue
		}

		// Create RepoInfo
		repoInfo := RepoInfo{
			FullName:      fullName,
			Path:          repo.GetPath(),
			URL:           repoURL,
			Description:   githubRepo.GetDescription(),
			Stars:         githubRepo.GetStargazersCount(),
			ReadmeContent: readmeContent,
			Language:      githubRepo.GetLanguage(),
			Icon:          githubRepo.GetOwner().GetAvatarURL(),
		}

		var repoFromDB RepoInfo
		err = db.QueryRow("SELECT readme_content, manifest, metadata FROM repositories WHERE full_name = $1", fullName).Scan(&repoFromDB.ReadmeContent, &repoFromDB.Manifest, &repoFromDB.Metadata)
		if err == nil {
			// Repository exists in DB, skip it
			log.Printf("Repository %s already exists in database, skipping", fullName)
			if repoFromDB.ReadmeContent == readmeContent && os.Getenv("RESCRAPE") != "true" {
				continue
			}
		}

		// Analyze repository with OpenAI
		analysis, err := analyzeWithOpenAI(fullName, readmeContent, repoFromDB.Manifest)
		if err != nil {
			log.Printf("Error analyzing repository %s: %v", fullName, err)
		} else {
			if len(analysis.Configs) == 0 {
				log.Printf("No MCP server found in repository %s", fullName)
				continue
			}

			manifestBytes, err := json.Marshal(analysis.Configs)
			if err != nil {
				log.Printf("Error marshaling manifest for repository %s: %v", fullName, err)
			} else {
				repoInfo.Manifest = string(manifestBytes)
			}

			metadata := map[string]string{}
			if repoInfo.Metadata != "" {
				err = json.Unmarshal([]byte(repoInfo.Metadata), &metadata)
				if err != nil {
					log.Printf("Error unmarshalling metadata for repository %s: %v", fullName, err)
				}
			}
			metadata["categories"] = analysis.Category
			metadataBytes, err := json.Marshal(metadata)
			if err != nil {
				log.Printf("Error marshaling metadata for repository %s: %v", fullName, err)
			} else {
				repoInfo.Metadata = string(metadataBytes)
			}
			repoInfo.Description = analysis.Description
			repoInfo.DisplayName = analysis.Name
		}

		saveRepo(repoInfo)
	}
}

func getReposHandler(w http.ResponseWriter, r *http.Request) {
	// Parse query parameters
	limit := 10000
	offset := 0
	sort := "stars"
	order := "desc"
	filter := r.URL.Query().Get("filter")

	limitParam := r.URL.Query().Get("limit")
	if limitParam != "" {
		if val, err := strconv.Atoi(limitParam); err == nil && val > 0 {
			limit = val
		}
	}

	offsetParam := r.URL.Query().Get("offset")
	if offsetParam != "" {
		if val, err := strconv.Atoi(offsetParam); err == nil && val >= 0 {
			offset = val
		}
	}

	sortParam := r.URL.Query().Get("sort")
	if sortParam != "" {
		// Validate sort parameter to prevent SQL injection
		validSorts := map[string]bool{"stars": true, "name": true, "id": true}
		if validSorts[sortParam] {
			sort = sortParam
		}
	}

	orderParam := r.URL.Query().Get("order")
	if orderParam != "" && (orderParam == "asc" || orderParam == "desc") {
		order = orderParam
	}

	// Build the query
	query := `
		SELECT id, path, full_name, display_name, url, description, stars, language, manifest, COALESCE(icon, ''), readme_content, metadata
		FROM repositories
	`
	countQuery := `SELECT COUNT(*) FROM repositories`

	var args []interface{}
	var whereClause string

	// Add the where clause to both queries
	if whereClause != "" {
		query += whereClause
		countQuery += whereClause
	}

	// Add sorting
	if sort == "name" {
		query += fmt.Sprintf(" ORDER BY full_name %s", order)
	} else {
		query += fmt.Sprintf(" ORDER BY %s %s", sort, order)
	}

	// Add pagination
	query += " LIMIT $" + strconv.Itoa(len(args)+1) + " OFFSET $" + strconv.Itoa(len(args)+2)
	args = append(args, limit, offset)

	// Get total count for pagination
	var totalCount int
	err := db.QueryRow(countQuery, args[:len(args)-2]...).Scan(&totalCount)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error counting repositories: %v", err), http.StatusInternalServerError)
		return
	}

	// Query repositories from the database with limit and offset
	rows, err := db.Query(query, args...)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error querying repositories: %v", err), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	overrideTotalCount := false

	// Parse the results
	repos := make([]RepoInfo, 0)
	for rows.Next() {
		var repo RepoInfo
		err := rows.Scan(
			&repo.ID,
			&repo.Path,
			&repo.FullName,
			&repo.DisplayName,
			&repo.URL,
			&repo.Description,
			&repo.Stars,
			&repo.Language,
			&repo.Manifest,
			&repo.Icon,
			&repo.ReadmeContent,
			&repo.Metadata,
		)
		if err != nil {
			http.Error(w, fmt.Sprintf("Error scanning repository: %v", err), http.StatusInternalServerError)
			return
		}

		if filter != "" && filter != "all" {
			var metadata map[string]string
			err = json.Unmarshal([]byte(repo.Metadata), &metadata)
			if err != nil {
				http.Error(w, fmt.Sprintf("Error unmarshalling metadata for repository %s: %v", repo.FullName, err), http.StatusInternalServerError)
			}

			if filter == "Featured" {
				if metadata["Featured"] == "true" {
					repos = append(repos, repo)
				}
			} else if filter == "Certified" {
				if metadata["Certified"] == "true" {
					repos = append(repos, repo)
				}
			}
			overrideTotalCount = true
		} else {
			repos = append(repos, repo)
		}
	}

	// Check for errors from iterating over rows
	if err := rows.Err(); err != nil {
		http.Error(w, fmt.Sprintf("Error iterating repositories: %v", err), http.StatusInternalServerError)
		return
	}

	// Set the total count in the response header
	if overrideTotalCount {
		totalCount = len(repos)
	}
	w.Header().Set("X-Total-Count", strconv.Itoa(totalCount))

	// Return the repositories as JSON
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(repos)
}

func searchReposHandler(w http.ResponseWriter, r *http.Request) {
	// Get search query from URL parameters
	query := r.URL.Query().Get("q")
	if query == "" {
		http.Error(w, "Search query is required", http.StatusBadRequest)
		return
	}

	// Prepare the search query for SQL
	searchQuery := "%" + query + "%"

	// Query repositories from the database that match the search query
	rows, err := db.Query(`
		SELECT id, path, full_name, display_name, url, description, stars, language, manifest, COALESCE(icon, ''), readme_content
		FROM repositories
		WHERE 
			description ILIKE $1
		ORDER BY stars DESC
	`, searchQuery)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error searching repositories: %v", err), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	// Parse the results
	repos := make([]RepoInfo, 0)
	for rows.Next() {
		var repo RepoInfo
		err := rows.Scan(
			&repo.ID,
			&repo.Path,
			&repo.FullName,
			&repo.DisplayName,
			&repo.URL,
			&repo.Description,
			&repo.Stars,
			&repo.Language,
			&repo.Manifest,
			&repo.Icon,
			&repo.ReadmeContent,
		)
		if err != nil {
			http.Error(w, fmt.Sprintf("Error scanning repository: %v", err), http.StatusInternalServerError)
			return
		}
		repos = append(repos, repo)
	}

	// Check for errors from iterating over rows
	if err := rows.Err(); err != nil {
		http.Error(w, fmt.Sprintf("Error iterating repositories: %v", err), http.StatusInternalServerError)
		return
	}

	// Return the repositories as JSON
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(repos)
}

func searchReposByReadmeHandler(w http.ResponseWriter, r *http.Request) {
	// Get search query from URL parameters
	query := r.URL.Query().Get("q")
	if query == "" {
		http.Error(w, "Search query is required", http.StatusBadRequest)
		return
	}

	// Prepare the search query for SQL
	searchQuery := "%" + query + "%"

	// Query repositories from the database that match the search query in readme content
	rows, err := db.Query(`
		SELECT id, path, full_name, display_name, url, description, stars, language, manifest, COALESCE(icon, ''), readme_content
		FROM repositories
		WHERE readme_content ILIKE $1
		ORDER BY stars DESC
	`, searchQuery)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error searching repositories by readme: %v", err), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	// Parse the results
	repos := make([]RepoInfo, 0)
	for rows.Next() {
		var repo RepoInfo
		err := rows.Scan(
			&repo.ID,
			&repo.Path,
			&repo.FullName,
			&repo.DisplayName,
			&repo.URL,
			&repo.Description,
			&repo.Stars,
			&repo.Language,
			&repo.Manifest,
			&repo.Icon,
			&repo.ReadmeContent,
		)
		if err != nil {
			http.Error(w, fmt.Sprintf("Error scanning repository: %v", err), http.StatusInternalServerError)
			return
		}
		repos = append(repos, repo)
	}

	// Check for errors from iterating over rows
	if err := rows.Err(); err != nil {
		http.Error(w, fmt.Sprintf("Error iterating repositories: %v", err), http.StatusInternalServerError)
		return
	}

	// Return the repositories as JSON
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(repos)
}

func generateConfigForSpecificRepoHandler(w http.ResponseWriter, r *http.Request) {
	if !isAuthorized(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	repoID := r.PathValue("id")

	// Check if repository exists and get its data
	var exists bool
	var existingID int
	var repo RepoInfo
	err := db.QueryRow(`
		SELECT EXISTS(
			SELECT 1 FROM repositories WHERE id = $1
		),
		COALESCE(id, 0),
		COALESCE(full_name, ''),
		COALESCE(display_name, ''),
		COALESCE(url, ''),
		COALESCE(description, ''),
		COALESCE(stars, 0),
		COALESCE(readme_content, ''),
		COALESCE(language, ''),
		COALESCE(manifest::text, ''),
		COALESCE(path, '')
		FROM repositories WHERE id = $1
	`, repoID).Scan(
		&exists,
		&existingID,
		&repo.FullName,
		&repo.DisplayName,
		&repo.URL,
		&repo.Description,
		&repo.Stars,
		&repo.ReadmeContent,
		&repo.Language,
		&repo.Manifest,
		&repo.Path,
	)
	if err != nil && err != sql.ErrNoRows {
		http.Error(w, fmt.Sprintf("Error checking repository existence: %v", err), http.StatusInternalServerError)
		return
	}

	if !exists {
		return
	}

	var readme string
	err = db.QueryRow("SELECT readme_content FROM repositories WHERE full_name = $1", repo.FullName).Scan(&readme)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error getting readme from database: %v", err), http.StatusInternalServerError)
		return
	}

	// Process the repository
	analysis, err := analyzeWithOpenAI(repo.FullName, readme, repo.Manifest)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error processing repository: %v", err), http.StatusInternalServerError)
		return
	}
	if len(analysis.Configs) == 0 {
		log.Printf("No MCP server found in repository %s", repo.FullName)
		return
	}

	manifestBytes, err := json.Marshal(analysis.Configs)
	if err != nil {
		log.Printf("Error marshaling manifest for repository %s: %v", repo.FullName, err)
	} else {
		repo.Manifest = string(manifestBytes)
	}

	metadata := map[string]string{}
	if repo.Metadata != "" {
		err = json.Unmarshal([]byte(repo.Metadata), &metadata)
		if err != nil {
			log.Printf("Error unmarshalling metadata for repository %s: %v", repo.FullName, err)
		}
	}
	metadata["categories"] = analysis.Category
	metadataBytes, err := json.Marshal(metadata)
	if err != nil {
		log.Printf("Error marshaling metadata for repository %s: %v", repo.FullName, err)
	} else {
		repo.Metadata = string(metadataBytes)
	}
	repo.Description = analysis.Description
	repo.DisplayName = analysis.Name

	// Insert or update the repository in the database
	var id int
	if exists {
		// Update existing repository
		_, err = db.Exec(`
			UPDATE repositories 
			SET url = $1, description = $2, stars = $3, readme_content = $4, language = $5, manifest = $6, path = $7, metadata = $8, display_name = $9
			WHERE id = $10
		`, repo.URL, repo.Description, repo.Stars, repo.ReadmeContent,
			repo.Language, repo.Manifest, repo.Path, repo.Metadata, repo.DisplayName, repoID)

		if err != nil {
			http.Error(w, fmt.Sprintf("Error updating repository: %v", err), http.StatusInternalServerError)
			return
		}
		id = existingID
	}

	// Return success response
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "success",
		"message": "Repository processed successfully",
		"id":      id,
	})
}

func getReposCountHandler(w http.ResponseWriter, r *http.Request) {
	// Parse query parameters for filtering
	filter := r.URL.Query().Get("filter")

	var query string
	var args []interface{}

	// Base query
	query = "SELECT COUNT(*) FROM repositories"

	// Add filter conditions if needed
	if filter != "" && filter != "all" {
		query += " WHERE category = $1"
		args = append(args, filter)
	}

	// Execute the count query
	var count int
	err := db.QueryRow(query, args...).Scan(&count)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error counting repositories: %v", err), http.StatusInternalServerError)
		return
	}

	// Return the count as JSON
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]int{"count": count})
}

func runMCPServerHandler(w http.ResponseWriter, r *http.Request) {
	if !isAuthorized(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Parse the repository ID from the URL
	urlPath := r.URL.Path
	parts := strings.Split(urlPath, "/")
	if len(parts) < 4 {
		http.Error(w, "Invalid URL format", http.StatusBadRequest)
		return
	}

	repoIDStr := parts[3]
	repoID, err := strconv.Atoi(repoIDStr)
	if err != nil {
		http.Error(w, "Invalid repository ID", http.StatusBadRequest)
		return
	}

	// Get repository information from the database
	var repo RepoInfo
	err = db.QueryRow(`
		SELECT id, path, full_name, url, description, stars, language, manifest, readme_content
		FROM repositories
		WHERE id = $1
	`, repoID).Scan(
		&repo.ID,
		&repo.Path,
		&repo.FullName,
		&repo.URL,
		&repo.Description,
		&repo.Stars,
		&repo.Language,
		&repo.Manifest,
		&repo.ReadmeContent,
	)

	if err != nil {
		http.Error(w, fmt.Sprintf("Error fetching repository: %v", err), http.StatusInternalServerError)
		return
	}

	type MCPServerRequest struct {
		MCPServers map[string]struct {
			Command     string            `json:"command"`
			Args        []string          `json:"args"`
			Env         map[string]string `json:"env,omitempty"`
			HTTPHeaders map[string]string `json:"httpHeaders,omitempty"`
			BaseURL     string            `json:"baseURL,omitempty"`
		} `json:"mcpServers"`
	}
	var mcpConfig MCPServerRequest

	err = json.NewDecoder(r.Body).Decode(&mcpConfig)
	if err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if len(mcpConfig.MCPServers) == 0 {
		http.Error(w, "No MCP server configurations provided", http.StatusBadRequest)
		return
	}

	// Get first server config
	var mcpServer struct {
		Command     string            `json:"command"`
		Args        []string          `json:"args"`
		Env         map[string]string `json:"env,omitempty"`
		HTTPHeaders map[string]string `json:"httpHeaders,omitempty"`
		BaseURL     string            `json:"baseURL,omitempty"`
	}
	for _, s := range mcpConfig.MCPServers {
		mcpServer = s
		break
	}

	var envSlice []string
	for key, value := range mcpServer.Env {
		envSlice = append(envSlice, fmt.Sprintf("%s=%s", key, value))
	}

	// Create MCP client
	mcpClient, err := client.NewStdioMCPClient(
		mcpServer.Command,
		envSlice,
		mcpServer.Args...,
	)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error creating MCP client: %v", err), http.StatusInternalServerError)
		return
	}
	defer mcpClient.Close()

	// Initialize the client
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	initRequest := mcp.InitializeRequest{}
	initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initRequest.Params.ClientInfo = mcp.Implementation{
		Name:    "mcp-proxy",
		Version: "1.0.0",
	}

	_, err = mcpClient.Initialize(ctx, initRequest)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error initializing MCP client: %v", err), http.StatusInternalServerError)
		return
	}

	// Get available tools
	toolsRequest := mcp.ListToolsRequest{}
	toolsResp, err := mcpClient.ListTools(ctx, toolsRequest)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error listing tools: %v", err), http.StatusInternalServerError)
		return
	}

	// Save tools to database
	toolsJSON, err := json.Marshal(toolsResp.Tools)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error marshaling tools: %v", err), http.StatusInternalServerError)
		return
	}

	_, err = db.Exec("UPDATE repositories SET tool_definitions = $1::jsonb WHERE id = $2", toolsJSON, repoID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error saving tools to database: %v", err), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(200)
	return
}

func getRepoHandler(w http.ResponseWriter, r *http.Request) {
	// Extract ID from path
	repoID := r.PathValue("id")

	// Query the database
	query := `
			SELECT id, path, full_name, display_name, url, description, stars, language, manifest, COALESCE(icon, ''), readme_content, COALESCE(tool_definitions, '{}'), COALESCE(metadata, '{}')
			FROM repositories 
			WHERE id = $1
		`
	row := db.QueryRow(query, repoID)

	var repo RepoInfo
	err := row.Scan(
		&repo.ID,
		&repo.Path,
		&repo.FullName,
		&repo.DisplayName,
		&repo.URL,
		&repo.Description,
		&repo.Stars,
		&repo.Language,
		&repo.Manifest,
		&repo.Icon,
		&repo.ReadmeContent,
		&repo.ToolDefinitions,
		&repo.Metadata,
	)

	if err == sql.ErrNoRows {
		http.Error(w, "Repository not found", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, fmt.Sprintf("Error fetching repository: %v", err), http.StatusInternalServerError)
		return
	}

	// Return the repository as JSON
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(repo)
}

func updateRepoHandler(w http.ResponseWriter, r *http.Request) {
	if !isAuthorized(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	repoID := r.PathValue("id")

	updatedManifest, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	query := `
		UPDATE repositories
		SET manifest = $1::jsonb
		WHERE id = $2
	`
	_, err = db.Exec(query, updatedManifest, repoID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error updating repository: %v", err), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(200)
}

func updateRepoMetadataHandler(w http.ResponseWriter, r *http.Request) {
	if !isAuthorized(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	repoID := r.PathValue("id")

	updatedMetadata, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	query := `
		UPDATE repositories
		SET metadata = $1::jsonb
		WHERE id = $2
	`
	_, err = db.Exec(query, updatedMetadata, repoID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error updating repository metadata: %v", err), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(200)
}

func startCronJobs() {
	c := cron.New()
	if os.Getenv("RESCRAPE") == "true" {
		go collectData()
	}

	// Schedule collectData() to run every day at midnight
	_, err := c.AddFunc("0 0 * * *", func() {
		log.Println("Running scheduled daily data collection...")
		go collectData()
	})
	if err != nil {
		log.Fatalf("Error scheduling cron job: %v", err)
	}

	c.Start()
}

func isAuthorized(r *http.Request) bool {
	cookie, err := r.Cookie("obot-catalog-server-token")
	if err != nil {
		return false
	}
	expected := os.Getenv("OBOT_CATALOG_SERVER_ACCESS_TOKEN")
	return cookie.Value == expected
}
