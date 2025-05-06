package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/obot-platform/catalog-service/pkg/types"
	"github.com/obot-platform/catalog-service/pkg/utils"
)

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
	repos := make([]types.RepoInfo, 0)
	for rows.Next() {
		var repo types.RepoInfo
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
	repos := make([]types.RepoInfo, 0)
	for rows.Next() {
		var repo types.RepoInfo
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
	repos := make([]types.RepoInfo, 0)
	for rows.Next() {
		var repo types.RepoInfo
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
	if !utils.IsAuthorized(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	repoID := r.PathValue("id")

	// Check if repository exists and get its data
	var exists bool
	var existingID int
	var repo types.RepoInfo
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
	analysis, err := utils.AnalyzeWithOpenAI(openaiClient, repo.FullName, readme, repo.Manifest)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error processing repository: %v", err), http.StatusInternalServerError)
		return
	}
	if len(analysis.Configs) == 0 {
		log.Printf("No MCP server found in repository %s", repo.FullName)
		return
	}

	utils.MarkPreferred(analysis.Configs)

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
	if !utils.IsAuthorized(r) {
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
	var repo types.RepoInfo
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

	var repo types.RepoInfo
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
	if !utils.IsAuthorized(r) {
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
	if !utils.IsAuthorized(r) {
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
