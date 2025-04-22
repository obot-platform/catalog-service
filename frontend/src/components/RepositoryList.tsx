import { useState, useEffect } from "react";
import { Link } from "react-router-dom";
import { Star, Code, ExternalLink } from "lucide-react";
import { Repository } from "../types";
import { Button } from "./ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "./ui/card";

const RepositoryList = () => {
  const [repositories, setRepositories] = useState<Repository[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const fetchData = async () => {
      try {
        // Fetch all repositories
        const response = await fetch("/api/repos");
        if (!response.ok) {
          throw new Error(
            `Failed to fetch repositories, error: ${response.statusText}`
          );
        }
        const data = await response.json();

        // Sort by stars
        const sortedRepos = [...data].sort((a, b) => b.stars - a.stars);
        setRepositories(sortedRepos);
        setLoading(false);
      } catch (err) {
        setError(
          err instanceof Error ? err.message : "An unknown error occurred"
        );
        setLoading(false);
      }
    };

    fetchData();
  }, []);

  if (loading) {
    return (
      <div className="flex h-[calc(100vh-3.5rem)] items-center justify-center">
        <div className="h-8 w-8 animate-spin rounded-full border-4 border-primary border-t-transparent"></div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="container py-10">
        <h2 className="text-2xl font-bold text-destructive">Error</h2>
        <p className="mt-2">{error}</p>
        <Button onClick={() => window.location.reload()} className="mt-4">
          Retry
        </Button>
      </div>
    );
  }

  return (
    <div className="container py-8">
      <h1 className="text-3xl font-bold tracking-tight mb-6">
        Repository Explorer
      </h1>

      <Card>
        <CardHeader>
          <CardTitle>All Repositories</CardTitle>
          <CardDescription>
            Popular repositories sorted by stars
          </CardDescription>
        </CardHeader>
        <CardContent>
          <div className="space-y-4">
            {repositories.map((repo) => (
              <div
                key={repo.id}
                className="flex items-start justify-between border-b pb-4"
              >
                <div className="space-y-1">
                  <Link
                    to={`/repository/${repo.id}`}
                    className="text-sm font-medium leading-none hover:underline flex items-center"
                  >
                    {repo.icon ? (
                      <img
                        src={repo.icon}
                        alt=""
                        className="h-4 w-4 mr-1.5 rounded-sm"
                      />
                    ) : (
                      <Code className="h-4 w-4 mr-1.5 text-muted-foreground" />
                    )}
                    {repo.displayName}
                  </Link>
                  <p className="text-sm text-muted-foreground line-clamp-2">
                    {repo.description || "No description available"}
                  </p>
                  <div className="flex items-center space-x-2 mt-2">
                    <span className="inline-flex items-center rounded-full bg-secondary px-2 py-0.5 text-xs font-medium">
                      <Star className="mr-1 h-3 w-3" />
                      {repo.stars}
                    </span>
                    {repo.metadata && (
                      <>
                        {JSON.parse(repo.metadata)
                          .categories?.split(",")
                          .map((cat: string) => cat.trim())
                          .filter(Boolean)
                          .map((cat: string, index: number) => (
                            <span
                              key={index}
                              className="inline-flex items-center rounded-full border border-primary/20 bg-primary/10 text-primary px-3 py-1 text-xs font-medium mr-2 shadow-sm hover:bg-primary/20 transition"
                            >
                              {cat}
                            </span>
                          ))}
                      </>
                    )}
                  </div>
                </div>
                <Button asChild variant="ghost" size="sm">
                  <a
                    href={repo.url}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="text-xs"
                  >
                    <ExternalLink className="h-3 w-3 mr-1" />
                    View
                  </a>
                </Button>
              </div>
            ))}
            <Button asChild variant="outline" size="sm" className="w-full mt-4">
              <Link to="/repositories">View all repositories with filters</Link>
            </Button>
          </div>
        </CardContent>
      </Card>
    </div>
  );
};

export default RepositoryList;
