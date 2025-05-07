package server

import (
	"context"
	"encoding/json"
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
	"github.com/sashabaranov/go-openai"
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
	for _, repo := range allRepos {
		owner := *repo.Repository.Owner.Login
		repoName := *repo.Repository.Name
		path := repo.GetPath()
		log.Printf("Processing repository: %s/%s/%s", owner, repoName, path)
		err := AddRepo(ctx, owner, repoName, path, force)
		if err != nil {
			log.Printf("Error processing repository %s: %v", *repo.Repository.FullName, err)
			continue
		}
	}
}

func AddRepo(ctx context.Context, owner string, repo string, path string, force bool) error {
	githubRepo, _, err := githubClient.Repositories.Get(ctx, owner, repo)
	if err != nil {
		return err
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
		return err
	}
	readmeContent, err = fileContent.GetContent()
	if err != nil {
		return err
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
		return fmt.Errorf("no MCP server found in repository %s", fullName)
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
	err = db.QueryRow("SELECT readme_content, manifest, metadata, tool_definitions FROM repositories WHERE full_name = $1", fullName).Scan(&repoFromDB.ReadmeContent, &repoFromDB.Manifest, &repoFromDB.Metadata, &repoFromDB.ToolDefinitions)
	if err == nil {
		// Repository exists in DB, skip it
		log.Printf("Repository %s already exists in database, skipping", fullName)
		if repoFromDB.ReadmeContent == readmeContent && !force {
			return nil
		}
	}

	// if manifest exists and it is not forced, update proposed_manifest instead
	proposed := true
	if (repoFromDB.Manifest == "" || repoFromDB.Manifest == "{}") || force {
		proposed = false
	}

	// Analyze repository with OpenAI
	analysis, err := utils.AnalyzeWithOpenAI(openaiClient, fullName, readmeContent, repoFromDB.Manifest)
	if err != nil {
		log.Printf("Error analyzing repository %s: %v", fullName, err)
	} else {
		if len(analysis.Configs) == 0 {
			return fmt.Errorf("no MCP server found in repository %s", fullName)
		}

		utils.MarkPreferred(analysis.Configs)

		manifestBytes, err := json.Marshal(analysis.Configs)
		if err != nil {
			return fmt.Errorf("error marshaling manifest for repository %s: %v", fullName, err)
		} else {
			if proposed {
				repoInfo.ProposedManifest = string(manifestBytes)
			} else {
				repoInfo.Manifest = string(manifestBytes)
			}
		}

		metadata := map[string]string{}
		if repoInfo.Metadata != "" {
			err = json.Unmarshal([]byte(repoInfo.Metadata), &metadata)
			if err != nil {
				return fmt.Errorf("error unmarshalling metadata for repository %s: %v", fullName, err)
			}
		}
		metadata["categories"] = analysis.Category
		metadataBytes, err := json.Marshal(metadata)
		if err != nil {
			return fmt.Errorf("error marshaling metadata for repository %s: %v", fullName, err)
		} else {
			repoInfo.Metadata = string(metadataBytes)
		}
		repoInfo.Description = analysis.Description
		repoInfo.DisplayName = analysis.Name
	}

	foundPreferred := false
	for _, config := range analysis.Configs {
		if config.Preferred {
			foundPreferred = true
			break
		}
	}

	if foundPreferred {
		if repoFromDB.ToolDefinitions == "" || repoFromDB.ToolDefinitions == "{}" || force {
			err = scrapeToolDefinitions(ctx, &repoInfo)
			if err != nil {
				return fmt.Errorf("error scraping tool definitions for repository %s: %v", fullName, err)
			}
		}
	}

	if repoInfo.ToolDefinitions == "" {
		repoInfo.ToolDefinitions = "{}"
	}

	return utils.SaveRepo(db, repoInfo, proposed)
}

func scrapeToolDefinitions(ctx context.Context, repo *types.RepoInfo) error {
	for {
		opts := &github.SearchOptions{
			ListOptions: github.ListOptions{
				PerPage: 1000,
			},
		}
		parts := strings.Split(repo.FullName, "/")

		if len(parts) < 2 {
			return fmt.Errorf("invalid repo name: %s", repo.FullName)
		}

		var allResults []*github.CodeResult

		query1 := fmt.Sprintf("tool extension:ts repo:%s/%s", parts[0], parts[1])

		result1, resp, err := githubClient.Search.Code(ctx, query1, opts)
		if err != nil {
			if _, ok := err.(*github.RateLimitError); ok {
				log.Printf("Hit rate limit, waiting for reset after time %s...\n", time.Until(resp.Rate.Reset.Time))
				time.Sleep(time.Until(resp.Rate.Reset.Time))
				continue
			}
			return err
		}

		allResults = append(allResults, result1.CodeResults...)

		query2 := fmt.Sprintf("mcp.tool extension:py repo:%s/%s", parts[0], parts[1])

		result2, resp, err := githubClient.Search.Code(ctx, query2, opts)
		if err != nil {
			if _, ok := err.(*github.RateLimitError); ok {
				log.Printf("Hit rate limit, waiting for reset after time %s...\n", time.Until(resp.Rate.Reset.Time))
				time.Sleep(time.Until(resp.Rate.Reset.Time))
				continue
			}
			return err
		}

		allResults = append(allResults, result2.CodeResults...)

		resultSet := make(map[string]*github.CodeResult)
		for _, codeResult := range allResults {
			resultSet[*codeResult.Repository.Owner.Login+"/"+*codeResult.Repository.Name+"/"+*codeResult.Path] = codeResult
		}

		filteredResults := make([]*github.CodeResult, 0)
		for _, codeResult := range resultSet {
			filteredResults = append(filteredResults, codeResult)
		}

		data := strings.Builder{}

		for _, codeResult := range filteredResults {
			prefix := strings.TrimSuffix(repo.Path, "README.md")
			if !strings.HasPrefix(*codeResult.Path, prefix) {
				continue
			}

			fileContent, _, _, err := githubClient.Repositories.GetContents(
				ctx,
				*codeResult.Repository.Owner.Login,
				*codeResult.Repository.Name,
				*codeResult.Path,
				nil,
			)
			if err != nil {
				return err
			}

			content, err := fileContent.GetContent()
			if err != nil {
				return err
			}

			data.WriteString(content)
		}

		prompt := fmt.Sprintf(`
		You are a helpful assistant that extracts tool definitions from a given code.
		Here is the code:
		%s

		Tool data should be in json format. return ToolResponse.

		type ToolResponse struct {
			Tools []MCPTool json:"tools"
		}

		type MCPTool struct {
			Name        string      json:"name"
			Description string      json:"description"
			InputSchema InputSchema json:"inputSchema,omitempty"
		}

		type InputSchema struct {
			Properties map[string]Property json:"properties"
		}

		type Property struct {
			Type        string json:"type"
			Description string json:"description"
			Required    bool   json:"required"
		}
		
		The tool description should be concise and to the point on what this tool is for.

		For typescript code, it can also be added through server.tool() method.

		For python code, it is also added through @mcp.tool() decorator.

		The properties description should be concise and to the point on what this tool parameter is for.

		If you can't find any tool definitions, try to fetch tool from readme. return an empty ToolResponse. Don't hallucinate. You have readme as %s.
		`, data.String(), repo.ReadmeContent)

		response, err := openaiClient.CreateChatCompletion(
			ctx,
			openai.ChatCompletionRequest{
				Model: openai.GPT4Dot1,
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
			return fmt.Errorf("error getting response from OpenAI: %v", err)
		}

		var tools types.ToolResponse
		err = json.Unmarshal([]byte(response.Choices[0].Message.Content), &tools)
		if err != nil {
			return fmt.Errorf("error unmarshalling tools: %v", err)
		}

		toolRaw, err := json.Marshal(tools.Tools)
		if err != nil {
			return fmt.Errorf("error marshalling tools: %v", err)
		}

		log.Printf("Updating Tool definitions for %s", repo.FullName)
		repo.ToolDefinitions = string(toolRaw)
		return nil
	}
}
