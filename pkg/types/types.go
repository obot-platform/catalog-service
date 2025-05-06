package types

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
	Env            []MCPPair `json:"env"`
	Command        string    `json:"command,omitempty"`
	Args           []string  `json:"args,omitempty"`
	HTTPHeaders    []MCPPair `json:"httpHeaders,omitempty"`
	URL            string    `json:"url,omitempty"`
	URLDescription string    `json:"urlDescription,omitempty"`
	Preferred      bool      `json:"preferred,omitempty"`
}

type MCPPair struct {
	Key         string `json:"key,omitempty"`
	Value       string `json:"value,omitempty"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
	Sensitive   bool   `json:"sensitive"`
}

type ToolResponse struct {
	Tools []MCPTool `json:"tools"`
}

type MCPTool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema InputSchema `json:"inputSchema,omitempty"`
}

type InputSchema struct {
	Properties map[string]Property `json:"properties"`
}

type Property struct {
	Type        string `json:"type"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
}
