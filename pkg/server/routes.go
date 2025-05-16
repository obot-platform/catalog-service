package server

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/go-github/v60/github"
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
			} else if filter == "Verified" {
				categories := metadata["categories"]
				parts := strings.Split(categories, ",")
				for _, part := range parts {
					if strings.TrimSpace(part) == "Verified" {
						repos = append(repos, repo)
						break
					}
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
			description ILIKE $1 OR
			display_name ILIKE $1
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

	force := r.URL.Query().Get("force") == "true"

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
		COALESCE(path, ''),
		COALESCE(proposed_manifest::text, '{}'),
		COALESCE(tool_definitions::text, '{}')
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
		&repo.ProposedManifest,
		&repo.ToolDefinitions,
	)
	if err != nil && err != sql.ErrNoRows {
		http.Error(w, fmt.Sprintf("Error checking repository existence: %v", err), http.StatusInternalServerError)
		return
	}

	if !exists {
		return
	}

	var readme string
	err = db.QueryRow("SELECT readme_content, metadata FROM repositories WHERE full_name = $1", repo.FullName).Scan(&readme, &repo.Metadata)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error getting readme from database: %v", err), http.StatusInternalServerError)
		return
	}

	if err := utils.UpdateRepo(r.Context(), repo, force, openaiClient, repo.FullName, readme, db, githubClient); err != nil {
		http.Error(w, fmt.Sprintf("Error updating repository: %v", err), http.StatusInternalServerError)
		return
	}

	// Return success response
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "success",
		"message": "Repository processed successfully",
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

func getRepoHandler(w http.ResponseWriter, r *http.Request) {
	// Extract ID from path
	repoID := r.PathValue("id")

	// Query the database
	query := `
			SELECT id, path, full_name, display_name, url, description, stars, language, manifest, COALESCE(icon, ''), readme_content, COALESCE(tool_definitions, '{}'), COALESCE(metadata, '{}'), COALESCE(proposed_manifest, '{}')
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
		&repo.ProposedManifest,
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

func rescrapeHandler(w http.ResponseWriter, r *http.Request) {
	if !utils.IsAuthorized(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	query := r.URL.Query().Get("force")
	force := query == "true"

	go collectData(force)

	w.WriteHeader(200)
}

func addRepoHandler(w http.ResponseWriter, r *http.Request) {
	if !utils.IsAuthorized(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var input struct {
		FullName string `json:"fullName"`
	}

	err := json.NewDecoder(r.Body).Decode(&input)
	if err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	parts := strings.Split(input.FullName, "/")
	if len(parts) < 3 {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	owner := parts[1]
	repo := parts[2]

	query := "mcpServers filename:README.md repo:" + owner + "/" + repo
	opts := &github.SearchOptions{
		ListOptions: github.ListOptions{
			PerPage: 1000,
		},
	}

	result, _, err := githubClient.Search.Code(r.Context(), query, opts)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error searching repositories: %v", err), http.StatusInternalServerError)
		return
	}

	var errs []error
	for _, codeResult := range result.CodeResults {
		owner := *codeResult.Repository.Owner.Login
		repoName := *codeResult.Repository.Name
		path := codeResult.GetPath()
		log.Printf("Processing repository: %s/%s/%s", owner, repoName, path)
		err := AddRepo(r.Context(), owner, repoName, path, false)
		if err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		http.Error(w, fmt.Sprintf("Error adding repositories: %v", errs), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(200)
}

func approveRepoHandler(w http.ResponseWriter, r *http.Request) {
	if !utils.IsAuthorized(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	repoID := r.PathValue("id")

	query := `
		UPDATE repositories
		SET manifest = proposed_manifest,
    		proposed_manifest = NULL
		WHERE id = $1
	`
	_, err := db.Exec(query, repoID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error approving repository: %v", err), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(200)
}
