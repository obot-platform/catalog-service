package utils

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/google/go-github/v60/github"
	"github.com/obot-platform/catalog-service/pkg/types"
	"github.com/sashabaranov/go-openai"
)

func IsAuthorized(r *http.Request) bool {
	cookie, err := r.Cookie("obot-catalog-server-token")
	if err != nil {
		return false
	}
	expected := os.Getenv("OBOT_CATALOG_SERVER_ACCESS_TOKEN")
	return cookie.Value == expected
}

func SaveRepo(db *sql.DB, repo types.RepoInfo, proposed bool) error {
	// Check if repository already exists
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM repositories WHERE full_name = $1", repo.FullName).Scan(&count)
	if err != nil {
		return fmt.Errorf("error checking if repository exists: %v", err)
	}

	if count > 0 {
		// Update existing repository
		if !proposed {
			log.Printf("Updating repository %s without proposed manifest", repo.FullName)
			_, err = db.Exec(`
			UPDATE repositories 
			SET url = $1, description = $2, display_name = $3, stars = $4, readme_content = $5, 
				language = $6, path = $7, manifest = $8::jsonb, icon = $9, metadata = $10::jsonb, tool_definitions = $11::jsonb, proposed_manifest = $12::jsonb
			WHERE full_name = $13
		`, repo.URL, repo.Description, repo.DisplayName, repo.Stars, repo.ReadmeContent,
				repo.Language, repo.Path, repo.Manifest, repo.Icon, repo.Metadata, repo.ToolDefinitions, "{}", repo.FullName)
		} else {
			log.Printf("Updating repository %s with proposed manifest", repo.FullName)
			_, err = db.Exec(`
			UPDATE repositories 
			SET url = $1, description = $2, display_name = $3, stars = $4, readme_content = $5, 
				language = $6, path = $7, proposed_manifest = $8::jsonb, icon = $9, metadata = $10::jsonb, tool_definitions = $11::jsonb
			WHERE full_name = $12
		`, repo.URL, repo.Description, repo.DisplayName, repo.Stars, repo.ReadmeContent,
				repo.Language, repo.Path, repo.ProposedManifest, repo.Icon, repo.Metadata, repo.ToolDefinitions, repo.FullName)
		}
		if err != nil {
			return fmt.Errorf("error updating repository %s: %v", repo.FullName, err)
		}
	} else {
		// Insert new repository
		if repo.Metadata == "" {
			repo.Metadata = "{}"
		}
		_, err = db.Exec(`
			INSERT INTO repositories 
			(full_name, url, description, display_name, stars, readme_content, language, path, manifest, icon, metadata, tool_definitions) 
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		`, repo.FullName, repo.URL, repo.Description, repo.DisplayName, repo.Stars, repo.ReadmeContent,
			repo.Language, repo.Path, []byte(repo.Manifest), repo.Icon, []byte(repo.Metadata), []byte(repo.ToolDefinitions))
		if err != nil {
			return fmt.Errorf("error inserting repository %s: %v", repo.FullName, err)
		}
	}
	return nil
}

func MarkPreferred(configs []types.MCPServerConfig) {
	var preferredIndex = -1

	// 1st priority: npx
	for i, cfg := range configs {
		if cfg.Command == "npx" {
			preferredIndex = i
			break
		}
	}

	// 2nd priority: uv or uvx
	if preferredIndex == -1 {
		for i, cfg := range configs {
			if cfg.Command == "uv" || cfg.Command == "uvx" {
				preferredIndex = i
				break
			}
		}
	}

	// 3rd priority: docker
	if preferredIndex == -1 {
		for i, cfg := range configs {
			if cfg.Command == "docker" {
				preferredIndex = i
				break
			}
		}
	}

	// Set the Prefer flag
	if preferredIndex != -1 {
		configs[preferredIndex].Preferred = true
	}
}

func AnalyzeWithOpenAI(openaiClient *openai.Client, repoName, readmeContent, existingConfig string) (types.MCPServerManifest, error) {
	var result types.MCPServerManifest

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
	URLDescription string    json:"urlDescription,omitempty"
}

type MCPPair struct {
	Key         string json:"key,omitempty"
	Value       string json:"value,omitempty"
	Name        string json:"name"
	Description string json:"description"
	Required    bool   json:"required"
	Sensitive   bool   json:"sensitive"
	File        bool   json:"file,omitempty"
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

If config has url, it means it is SSE based MCP server. You should only populate url, urlDescription and headers. 
If config has command, it means it is CLI based MCP server. You should only populate command, args and env.

When looking for Env in MCPServerConfig, The key of the environment variable and usually starts with UPPERCASE.
The name of the environment variable is usually a friendly name representing the environment variable and it is usually starts with lowercase. File should be true if the value of the environment variable refers to a file path.
If you can't find any environment variables, you can return empty array for env. don't hallucinate.

The description from OpenAIResponse should be concise and to the point on what this MCP server is for.

Make sure you can extract command, args and env from the mcp config example in the readme.
It is usually wrapped into json block. For other MCPPair, you should look in the readme to find possible explaination.

Return OpenAIResponse which contains a list of MCPServerManifest which supports docker, npx and uv and a category.

`, repoName, readmeContent)

	// Call OpenAI API
	resp, err := openaiClient.CreateChatCompletion(
		context.Background(),
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

func UpdateRepo(ctx context.Context, repo types.RepoInfo, force bool, openaiClient *openai.Client, fullName, readmeContent string, db *sql.DB, githubClient *github.Client) error {
	// if manifest exists and it is not forced, update proposed_manifest instead
	proposed := true
	if (repo.Manifest == "" || repo.Manifest == "{}") || force {
		proposed = false
	}

	// Analyze repository with OpenAI
	analysis, err := AnalyzeWithOpenAI(openaiClient, fullName, readmeContent, repo.Manifest)
	if err != nil {
		log.Printf("Error analyzing repository %s: %v", fullName, err)
	} else {
		if len(analysis.Configs) == 0 {
			return fmt.Errorf("no MCP server found in repository %s", fullName)
		}

		MarkPreferred(analysis.Configs)

		manifestBytes, err := json.Marshal(analysis.Configs)
		if err != nil {
			return fmt.Errorf("error marshaling manifest for repository %s: %v", fullName, err)
		} else {
			if proposed {
				repo.ProposedManifest = string(manifestBytes)
			} else {
				repo.Manifest = string(manifestBytes)
			}
		}

		metadata := map[string]string{}
		if repo.Metadata != "" {
			err = json.Unmarshal([]byte(repo.Metadata), &metadata)
			if err != nil {
				return fmt.Errorf("error unmarshalling metadata for repository %s: %v", fullName, err)
			}
		}
		verified := false
		existingCategories := strings.Split(metadata["categories"], ",")
		if !slices.Contains(existingCategories, "Verified") {
			verified = true
		}
		categories := analysis.Category
		if verified {
			categories = categories + ",Verified"
		}
		metadata["categories"] = categories
		metadataBytes, err := json.Marshal(metadata)
		if err != nil {
			return fmt.Errorf("error marshaling metadata for repository %s: %v", fullName, err)
		} else {
			repo.Metadata = string(metadataBytes)
		}
		repo.Description = analysis.Description
		repo.DisplayName = analysis.Name
	}

	foundPreferred := false
	for _, config := range analysis.Configs {
		if config.Preferred {
			foundPreferred = true
			break
		}
	}

	if foundPreferred {
		if repo.ToolDefinitions == "" || repo.ToolDefinitions == "{}" || force {
			err = ScrapeToolDefinitions(ctx, &repo, db, githubClient, openaiClient)
			if err != nil {
				return fmt.Errorf("error scraping tool definitions for repository %s: %v", fullName, err)
			}
		}
	}

	if repo.ToolDefinitions == "" {
		repo.ToolDefinitions = "{}"
	}

	return SaveRepo(db, repo, proposed)

}

func ScrapeToolDefinitions(ctx context.Context, repo *types.RepoInfo, db *sql.DB, githubClient *github.Client, openaiClient *openai.Client) error {
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
