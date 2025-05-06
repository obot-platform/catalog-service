export interface Repository {
  id: number;
  icon: string;
  displayName: string;
  fullName: string;
  url: string;
  description: string;
  stars: number;
  language: string;
  summary: string;
  source: string;
  readmeContent: string;
  manifest: string;
  metadata: string;
  toolDefinitions: string;
}

export interface Stats {
  totalRepos: number;
  officialRepos: number;
  searchRepos: number;
  readmeRepos: number;
  topStar: number;
  avgStar: number;
  categories: Record<string, number>;
}

export interface MCPTool {
  name: string;
  description: string;
  inputSchema?: InputSchema;
}

export interface InputSchema {
  properties: Record<string, Property>;
}

export interface Property {
  type: string;
  description: string;
  required: boolean;
}
