package utils

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"

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
