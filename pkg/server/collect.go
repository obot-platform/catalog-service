package server

import (
	"context"
	"fmt"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/go-github/v60/github"
	"github.com/obot-platform/catalog-service/pkg/types"
	"github.com/obot-platform/catalog-service/pkg/utils"
	"github.com/robfig/cron/v3"
)

func startCronJobs() {
	c := cron.New()

	// Schedule collectData() to run every day at midnight
	_, err := c.AddFunc("0 0 * * *", func() {
		log.Println("Running scheduled daily data collection...")
		go collectData(false)
	})
	if err != nil {
		log.Fatalf("Error scheduling cron job: %v", err)
	}

	c.Start()
}

func collectData(force bool) {
	ctx := context.Background()
	log.Println("Searching repositories by README content...")
	limit, _ := strconv.Atoi(os.Getenv("LIMIT"))
	if limit == 0 {
		limit = 4000
	}
	searchReposByReadme(ctx, limit, force)
}

func searchReposByReadme(ctx context.Context, limit int, force bool) {
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
	addedRepos := make(map[string]bool)
	for _, repo := range allRepos {
		owner := *repo.Repository.Owner.Login
		repoName := *repo.Repository.Name
		path := repo.GetPath()
		log.Printf("Processing repository: %s/%s/%s", owner, repoName, path)
		addedRepoName, err := AddRepo(ctx, owner, repoName, path, force)
		if err != nil {
			log.Printf("Error processing repository %s: %v", *repo.Repository.FullName, err)
			continue
		}
		addedRepos[addedRepoName] = true
	}

	if force {
		query := `
		SELECT id, full_name, display_name, url, description, stars, readme_content, language, manifest, path, COALESCE(proposed_manifest, '{}'), COALESCE(tool_definitions, '{}'), COALESCE(icon, '')
		FROM repositories
	`
		rows, err := db.Query(query)
		if err != nil {
			log.Fatalf("Error querying repositories: %v", err)
		}
		defer rows.Close()

		for rows.Next() {
			var repo types.RepoInfo
			err := rows.Scan(&repo.ID,
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
				&repo.Icon)
			if err != nil {
				log.Fatalf("Error scanning repository: %v", err)
			}
			if !addedRepos[repo.FullName] {
				var readme string
				var metadata string
				err = db.QueryRow("SELECT readme_content, metadata FROM repositories WHERE full_name = $1", repo.FullName).Scan(&readme, &metadata)
				if err != nil {
					log.Fatalf("Error getting readme from database: %v", err)
					return
				}

				log.Printf("Updating repository: %s from existing database", repo.FullName)

				if _, err := utils.UpdateRepo(ctx, repo, force, openaiClient, repo.FullName, readme, db, githubClient); err != nil {
					log.Fatalf("Error updating repository: %v", err)
					return
				}
			}
		}
	}
}

func AddRepo(ctx context.Context, owner string, repo string, path string, force bool) (string, error) {
	githubRepo, _, err := githubClient.Repositories.Get(ctx, owner, repo)
	if err != nil {
		return "", err
	}

	// Get README content from the specific path where it was found
	readmeContent := ""
	fileContent, _, _, err := githubClient.Repositories.GetContents(
		ctx,
		*githubRepo.Owner.Login,
		*githubRepo.Name,
		path,
		nil,
	)
	if err != nil {
		return "", err
	}
	readmeContent, err = fileContent.GetContent()
	if err != nil {
		return "", err
	}

	fullName := *githubRepo.FullName
	parts := strings.Split(path, "/")
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
		return "", fmt.Errorf("no MCP server found in repository %s", fullName)
	}

	// Create RepoInfo
	repoInfo := types.RepoInfo{
		FullName:      fullName,
		Path:          path,
		URL:           repoURL,
		Description:   githubRepo.GetDescription(),
		Stars:         githubRepo.GetStargazersCount(),
		ReadmeContent: readmeContent,
		Language:      githubRepo.GetLanguage(),
		Icon:          githubRepo.GetOwner().GetAvatarURL(),
	}

	var repoFromDB types.RepoInfo
	err = db.QueryRow("SELECT readme_content, manifest, metadata, tool_definitions, icon FROM repositories WHERE full_name = $1", fullName).Scan(&repoFromDB.ReadmeContent, &repoFromDB.Manifest, &repoFromDB.Metadata, &repoFromDB.ToolDefinitions, &repoFromDB.Icon)
	if err == nil {
		if repoFromDB.ReadmeContent == readmeContent && !force {
			// Repository exists in DB, skip it, unless it doesn't have an icon and we need to add it.
			if repoFromDB.Icon == "" {
				// now update in db
				db.Exec("UPDATE repositories SET icon = $1 WHERE full_name = $2", githubRepo.GetOwner().GetAvatarURL(), fullName)
				log.Printf("Updated icon for repository %s", fullName)
			}

			log.Printf("Repository %s already exists in database, skipping", fullName)
			return "", nil
		}
	}
	repoInfo.Metadata = repoFromDB.Metadata

	return utils.UpdateRepo(ctx, repoInfo, force, openaiClient, fullName, readmeContent, db, githubClient)
}
