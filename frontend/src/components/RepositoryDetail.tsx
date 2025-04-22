import { useParams, Link } from "react-router-dom";
import { useState, useEffect } from "react";
import { ArrowLeft, ExternalLink, Star } from "lucide-react";
import { Prism as SyntaxHighlighter } from "react-syntax-highlighter";
import { vscDarkPlus } from "react-syntax-highlighter/dist/esm/styles/prism";
import { Repository, MCPTool } from "../types";
import { Button } from "./ui/button";
import Markdown from "react-markdown";
import { useToast } from "../hooks/use-toast";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "./ui/card";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "./ui/tabs";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "./ui/dialog";
import { Label } from "./ui/label";
import { Textarea } from "./ui/textarea";
import "github-markdown-css/github-markdown.css";
import React from "react";

const RepositoryDetail = () => {
  const { id } = useParams<{ id: string }>();
  const [repository, setRepository] = useState<Repository | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [runDialogOpen, setRunDialogOpen] = useState(false);
  const [tools, setTools] = useState<MCPTool[]>([]);
  const [configText, setConfigText] = useState("");
  const [configError, setConfigError] = useState<string | null>(null);
  const [metadata, setMetadata] = useState<Record<string, string>>({});
  const [newMetaKey, setNewMetaKey] = useState("");
  const [newMetaValue, setNewMetaValue] = useState("");
  const { toast } = useToast();

  useEffect(() => {
    const fetchRepository = async () => {
      try {
        const response = await fetch(`/api/repo/${id}`);
        if (!response.ok) {
          throw new Error("Failed to fetch repository details");
        }
        const repo = await response.json();

        setRepository(repo);

        if (repo.toolDefinitions) {
          try {
            const parsedTools = JSON.parse(repo.toolDefinitions);
            setTools(parsedTools);
          } catch (e) {
            console.error("Failed to parse tool definitions:", e);
          }
        }

        if (repo.manifest) {
          const parsed = JSON.parse(repo.manifest);
          const formatted = JSON.stringify(parsed, null, 2);
          setConfigText(formatted);
          setConfigError(null);
        }

        if (repo.metadata) {
          setMetadata(JSON.parse(repo.metadata));
        }

        setLoading(false);
      } catch (err) {
        setError(
          err instanceof Error ? err.message : "An unknown error occurred"
        );
        setLoading(false);
      }
    };

    fetchRepository();
  }, [id]);

  // Process README content to fix relative image URLs
  const processReadmeContent = (content: string) => {
    if (!content) return "";

    // Extract the owner and repo name from fullName (format: owner/repo)
    const [owner, repo] = repository?.fullName.split("/") || [];

    // Replace relative image URLs with absolute GitHub URLs
    return content.replace(
      /!\[(.*?)\]\(((?!http|https:\/\/)(.*?))\)/g,
      (_, alt, relativePath) => {
        // If the path starts with ./ or ../, it's a relative path
        if (relativePath.startsWith("./") || relativePath.startsWith("../")) {
          return `![${alt}](https://raw.githubusercontent.com/${owner}/${repo}/main/${relativePath.replace(
            /^\.\//,
            ""
          )})`;
        }
        // If the path is just a filename, assume it's in the root
        return `![${alt}](https://raw.githubusercontent.com/${owner}/${repo}/main/${relativePath})`;
      }
    );
  };

  const updateMetadata = async (updatedMetadata: Record<string, string>) => {
    try {
      const response = await fetch(`/api/repo/${id}/metadata`, {
        method: "PUT",
        headers: {
          "Content-Type": "application/json",
        },
        body: JSON.stringify(updatedMetadata),
      });

      if (!response.ok) {
        throw new Error(response.statusText);
      }

      toast({
        title: "Metadata Saved",
        description: "The metadata was updated successfully.",
      });
    } catch (error) {
      toast({
        title: "Save Failed",
        description: `There was an error saving the metadata. Error: ${error}`,
        variant: "destructive",
      });
    }
  };

  // Memoize the processed readme content
  const processedReadmeContent = React.useMemo(() => {
    return repository ? processReadmeContent(repository.readmeContent) : "";
  }, [repository]);

  if (loading) {
    return (
      <div className="flex h-[calc(100vh-3.5rem)] items-center justify-center">
        <div className="h-8 w-8 animate-spin rounded-full border-4 border-primary border-t-transparent"></div>
      </div>
    );
  }

  if (error || !repository) {
    return (
      <div className="container py-10">
        <h2 className="text-2xl font-bold text-destructive">Error</h2>
        <p className="mt-2">{error || "Repository not found"}</p>
        <Button asChild className="mt-4">
          <Link to="/repositories">
            <ArrowLeft className="mr-2 h-4 w-4" />
            Back to Repositories
          </Link>
        </Button>
      </div>
    );
  }

  return (
    <div className="container py-8">
      <Card className="mb-6">
        <CardContent className="pt-6">
          <div className="flex justify-between items-start">
            <div>
              <Button asChild variant="outline" size="sm" className="mb-4">
                <Link to="/">
                  <ArrowLeft className="mr-2 h-4 w-4" />
                  Back to Dashboard
                </Link>
              </Button>
              <h1 className="text-2xl font-bold tracking-tight mb-2">
                {repository.displayName}
              </h1>
              <p className="text-muted-foreground mb-4">
                {repository.description || "No description available"}
              </p>
              <div className="flex flex-wrap gap-2">
                <span className="inline-flex items-center rounded-full bg-secondary px-2.5 py-0.5 text-xs font-medium">
                  <Star className="mr-1 h-3 w-3" />
                  {repository.stars}
                </span>
                {metadata.categories &&
                  metadata.categories
                    .split(",")
                    .map((cat: string) => cat.trim())
                    .filter(Boolean)
                    .map((cat: string, index: number) => (
                      <span
                        key={index}
                        className="inline-flex items-center rounded-full bg-primary/10 text-primary px-2.5 py-0.5 text-xs font-medium mr-2"
                      >
                        {cat}
                      </span>
                    ))}
              </div>
            </div>
            <div className="flex flex-col gap-2">
              <Button asChild variant="outline" size="sm">
                <a
                  href={repository.url}
                  target="_blank"
                  rel="noopener noreferrer"
                >
                  <ExternalLink className="mr-2 h-4 w-4" />
                  View on GitHub
                </a>
              </Button>
            </div>
          </div>
        </CardContent>
      </Card>

      <Tabs defaultValue="readme" className="mb-8">
        <TabsList className="flex justify-start">
          <TabsTrigger value="readme">README</TabsTrigger>
          <TabsTrigger value="manifest">Configuration</TabsTrigger>
          {/* <TabsTrigger value="tools" disabled={tools.length === 0}>
            Tools {tools.length > 0 && `(${tools.length})`}
          </TabsTrigger> */}
          <TabsTrigger value="metadata">Metadata</TabsTrigger>
        </TabsList>

        <TabsContent value="readme" className="mt-6">
          <Card>
            <CardHeader>
              <CardTitle>README</CardTitle>
            </CardHeader>
            <CardContent>
              {repository.readmeContent ? (
                <Markdown className="markdown-body">
                  {processedReadmeContent}
                </Markdown>
              ) : (
                <p className="text-muted-foreground">
                  No README content available for this repository.
                </p>
              )}
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="manifest" className="mt-6">
          <Card>
            <CardHeader>
              <CardTitle>Configuration</CardTitle>
            </CardHeader>
            <CardContent>
              {repository.manifest ? (
                <>
                  <SyntaxHighlighter
                    language="json"
                    style={vscDarkPlus}
                    className="rounded-md text-sm"
                    wrapLines
                  >
                    {configText}
                  </SyntaxHighlighter>
                  <Button
                    onClick={() => setRunDialogOpen(true)}
                    className="mt-4"
                    variant="outline"
                  >
                    Edit Configuration
                  </Button>
                  <Dialog open={runDialogOpen} onOpenChange={setRunDialogOpen}>
                    <DialogContent className="sm:max-w-[600px]">
                      <DialogHeader>
                        <DialogTitle>Edit Configuration</DialogTitle>
                        <DialogDescription>
                          Modify the configuration JSON for this repository
                        </DialogDescription>
                      </DialogHeader>
                      <div className="grid gap-4 py-4">
                        <Label htmlFor="config-textarea">
                          Configuration JSON
                        </Label>
                        <Textarea
                          id="config-textarea"
                          className="font-mono text-sm"
                          rows={20}
                          value={configText}
                          onChange={(e) => {
                            const parsed = JSON.parse(e.target.value);
                            const formatted = JSON.stringify(parsed, null, 2);
                            setConfigText(formatted);
                            setConfigError(null);
                          }}
                          onBlur={() => {
                            try {
                              const parsed = JSON.parse(configText);
                              const formatted = JSON.stringify(parsed, null, 2);
                              setConfigText(formatted);
                              setConfigError(null);
                            } catch {
                              setConfigError("Invalid JSON format.");
                            }
                          }}
                        />
                        {configError && (
                          <p className="mt-2 text-sm text-destructive">
                            {configError}
                          </p>
                        )}
                      </div>
                      <DialogFooter>
                        <Button
                          onClick={() => setRunDialogOpen(false)}
                          variant="outline"
                        >
                          Cancel
                        </Button>
                        <Button
                          onClick={async () => {
                            try {
                              const manifest = JSON.parse(configText);
                              const response = await fetch(`/api/repo/${id}`, {
                                method: "PUT",
                                headers: {
                                  "Content-Type": "application/json",
                                },
                                body: JSON.stringify(manifest),
                              });

                              if (!response.ok) {
                                throw new Error(response.statusText);
                              }

                              toast({
                                title: "Metadata Saved",
                                description:
                                  "The metadata was updated successfully.",
                              });
                              setConfigError(null);
                              setRunDialogOpen(false);
                            } catch (error) {
                              toast({
                                title: "Save Failed",
                                description: `There was an error saving the metadata. Error: ${error}`,
                                variant: "destructive",
                              });
                            }
                          }}
                          disabled={!!configError}
                        >
                          Save
                        </Button>
                      </DialogFooter>
                    </DialogContent>
                  </Dialog>
                </>
              ) : (
                <p className="text-muted-foreground">
                  No configuration available for this repository.
                </p>
              )}
            </CardContent>
            ...
          </Card>
        </TabsContent>

        <TabsContent value="tools" className="mt-6">
          <Card>
            <CardHeader>
              <CardTitle>MCP Tools</CardTitle>
              <CardDescription>
                Tools provided by this MCP server
              </CardDescription>
            </CardHeader>
            <CardContent>
              {tools.length > 0 ? (
                <div className="space-y-6">
                  {tools.map((tool, index) => (
                    <div key={index} className="border rounded-lg p-4">
                      <h3 className="text-lg font-semibold mb-2">
                        {tool.name}
                      </h3>
                      <p className="text-sm text-muted-foreground mb-4">
                        {tool.description}
                      </p>

                      {tool.inputSchema && (
                        <div className="mt-4">
                          <h4 className="text-sm font-medium mb-2">
                            Input Schema
                          </h4>

                          {/* Display required properties */}
                          {tool.inputSchema.required &&
                            tool.inputSchema.required.length > 0 && (
                              <div className="mb-2">
                                <p className="text-xs font-medium text-muted-foreground">
                                  Required Parameters:
                                </p>
                                <div className="flex flex-wrap gap-1 mt-1">
                                  {tool.inputSchema.required.map(
                                    (param: string) => (
                                      <span
                                        key={param}
                                        className="inline-flex items-center rounded-full bg-primary/10 text-primary px-2 py-0.5 text-xs font-medium"
                                      >
                                        {param}
                                      </span>
                                    )
                                  )}
                                </div>
                              </div>
                            )}

                          {/* Display properties table */}
                          {tool.inputSchema.properties &&
                            Object.keys(tool.inputSchema.properties).length >
                              0 && (
                              <div className="border rounded-md overflow-hidden mt-2">
                                <table className="w-full text-sm">
                                  <thead className="bg-muted">
                                    <tr>
                                      <th className="px-4 py-2 text-left font-medium">
                                        Parameter
                                      </th>
                                      <th className="px-4 py-2 text-left font-medium">
                                        Type
                                      </th>
                                      <th className="px-4 py-2 text-left font-medium">
                                        Description
                                      </th>
                                    </tr>
                                  </thead>
                                  <tbody className="divide-y">
                                    {Object.entries(
                                      tool.inputSchema.properties
                                    ).map(([name, prop]: [string, any]) => (
                                      <tr
                                        key={name}
                                        className="hover:bg-muted/50"
                                      >
                                        <td className="px-4 py-2 font-mono text-xs">
                                          {name}
                                          {tool.inputSchema.required &&
                                            tool.inputSchema.required.includes(
                                              name
                                            ) && (
                                              <span className="text-destructive ml-1">
                                                *
                                              </span>
                                            )}
                                        </td>
                                        <td className="px-4 py-2 font-mono text-xs">
                                          {prop.type}
                                        </td>
                                        <td className="px-4 py-2 text-xs">
                                          {prop.description}
                                        </td>
                                      </tr>
                                    ))}
                                  </tbody>
                                </table>
                              </div>
                            )}

                          {/* Full schema (collapsible) */}
                          <details className="mt-3">
                            <summary className="text-xs font-medium cursor-pointer">
                              View full schema
                            </summary>
                            <SyntaxHighlighter
                              language="json"
                              style={vscDarkPlus}
                              className="rounded-md text-xs mt-2"
                            >
                              {JSON.stringify(tool.inputSchema, null, 2)}
                            </SyntaxHighlighter>
                          </details>
                        </div>
                      )}

                      {/* Display annotations if present */}
                      {tool.annotations &&
                        Object.keys(tool.annotations).length > 0 && (
                          <div className="mt-4">
                            <h4 className="text-sm font-medium mb-2">
                              Annotations
                            </h4>
                            <div className="bg-muted rounded-md p-2">
                              <code className="text-xs">
                                {JSON.stringify(tool.annotations, null, 2)}
                              </code>
                            </div>
                          </div>
                        )}

                      {/* Display authentication if present */}
                      {tool.auth && Object.keys(tool.auth).length > 0 && (
                        <div className="mt-4">
                          <h4 className="text-sm font-medium mb-2">
                            Authentication
                          </h4>
                          <div className="bg-muted rounded-md p-2">
                            <code className="text-xs">
                              {JSON.stringify(tool.auth, null, 2)}
                            </code>
                          </div>
                        </div>
                      )}
                    </div>
                  ))}
                </div>
              ) : (
                <p className="text-muted-foreground">No tools available.</p>
              )}
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="metadata" className="mt-6">
          <Card>
            <CardHeader>
              <CardTitle>Metadata</CardTitle>
              <CardDescription>
                Key-value pairs attached to the repository
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              {Object.entries(metadata).length > 0 ? (
                Object.entries(metadata).map(([key, value]) => (
                  <div key={key} className="flex items-center gap-2">
                    <input
                      type="text"
                      className="input border px-2 py-1 w-1/3"
                      value={key}
                      readOnly
                    />
                    <input
                      type="text"
                      className="input border px-2 py-1 w-1/2 text-muted-foreground"
                      value={value}
                      readOnly
                    />
                    <Button
                      variant="destructive"
                      size="sm"
                      onClick={() => {
                        const updated = { ...metadata };
                        delete updated[key];
                        setMetadata(updated);
                        updateMetadata(updated);
                      }}
                    >
                      Remove
                    </Button>
                  </div>
                ))
              ) : (
                <p className="text-muted-foreground">No metadata defined.</p>
              )}

              <div className="border-t pt-4 space-y-2">
                <h4 className="text-sm font-medium">Add New Metadata</h4>
                <div className="flex gap-2">
                  <input
                    type="text"
                    placeholder="Key"
                    className="input border px-2 py-1 w-1/3"
                    value={newMetaKey}
                    onChange={(e) => setNewMetaKey(e.target.value)}
                  />
                  <input
                    type="text"
                    placeholder="Value"
                    className="input border px-2 py-1 w-1/2"
                    value={newMetaValue}
                    onChange={(e) => setNewMetaValue(e.target.value)}
                  />
                  <Button
                    size="sm"
                    onClick={() => {
                      if (!newMetaKey) return;
                      const updated = { ...metadata };
                      updated[newMetaKey] = newMetaValue;
                      setMetadata(updated);
                      updateMetadata(updated);
                      setNewMetaKey("");
                      setNewMetaValue("");
                    }}
                  >
                    Add
                  </Button>
                </div>
              </div>
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>
    </div>
  );
};

export default RepositoryDetail;
