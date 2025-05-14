import { useState, useEffect } from "react";
import { useLocation, Link, useNavigate } from "react-router-dom";
import {
  Star,
  Code,
  Filter,
  SortAsc,
  SortDesc,
  ExternalLink,
  Loader,
} from "lucide-react";
import { Repository } from "../types";
import { Button } from "./ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "./ui/card";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "./ui/select";

const ITEMS_PER_PAGE = 12;

const Dashboard = () => {
  const [repositories, setRepositories] = useState<Repository[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [page, setPage] = useState(1);
  const [totalCount, setTotalCount] = useState(0);
  const [filter, setFilter] = useState("all");
  const [sort, setSort] = useState("stars");
  const [sortDirection, setSortDirection] = useState("desc");
  const [isCodeSearching, setIsCodeSearching] = useState(false);

  const location = useLocation();
  const navigate = useNavigate();
  const searchParams = new URLSearchParams(location.search);
  const searchQuery = searchParams.get("q");
  const codeSearch = searchParams.get("codeSearch") === "true";
  const viewAll = searchParams.get("viewAll") === "true";
  const filterParam = searchParams.get("filter");
  const pageParam = searchParams.get("page");

  // Initialize page from URL if present
  useEffect(() => {
    if (pageParam) {
      const pageNumber = parseInt(pageParam, 10);
      if (!isNaN(pageNumber) && pageNumber > 0) {
        setPage(pageNumber);
      }
    }
    if (filterParam) {
      setFilter(filterParam);
    } else {
      setFilter("all");
    }
  }, [pageParam, filterParam]);

  useEffect(() => {
    const fetchRepositories = async () => {
      setLoading(true);
      try {
        let url;
        let isCountNeeded = true;

        if (viewAll) {
          url = "/api/repos";
          isCountNeeded = false;
        } else if (codeSearch) {
          setIsCodeSearching(true);
          url = `/api/search-readme?q=${encodeURIComponent(searchQuery || "")}`;
          isCountNeeded = false;
        } else if (searchQuery) {
          url = `/api/search?q=${encodeURIComponent(searchQuery)}`;
          isCountNeeded = false;
        } else {
          const offset = (page - 1) * ITEMS_PER_PAGE;
          url = `/api/repos?limit=${ITEMS_PER_PAGE}&offset=${offset}`;

          // Add sort parameters
          if (sort) {
            url += `&sort=${sort}&order=${sortDirection}`;
          }

          // Add filter parameter if not "all"
          if (filter !== "all") {
            url += `&filter=${filter}`;
            isCountNeeded = true;
          }
        }

        const response = await fetch(url);

        if (!response.ok) {
          throw new Error("Failed to fetch repositories");
        }

        const data = await response.json();

        // If we're using the paginated API, the response should include total count
        if (response.headers.get("X-Total-Count")) {
          setTotalCount(
            parseInt(response.headers.get("X-Total-Count") || "0", 10)
          );
        } else if (isCountNeeded) {
          // If the API doesn't return a count, we'll need to fetch it separately
          const countResponse = await fetch("/api/repos/count");
          if (countResponse.ok) {
            const countData = await countResponse.json();
            setTotalCount(countData.count);
          }
        } else {
          // For non-paginated responses, use the length of the data
          setTotalCount(data.length);
        }

        setRepositories(data);
        setLoading(false);
        setIsCodeSearching(false);
      } catch (err) {
        setError(
          err instanceof Error ? err.message : "An unknown error occurred"
        );
        setLoading(false);
        setIsCodeSearching(false);
      }
    };

    fetchRepositories();
  }, [page, filter, sort, sortDirection, searchQuery, codeSearch, viewAll]);

  const handlePageChange = (newPage: number) => {
    // Update URL with the new page
    const newSearchParams = new URLSearchParams(searchParams);
    newSearchParams.set("page", newPage.toString());
    navigate(`${location.pathname}?${newSearchParams.toString()}`);

    setPage(newPage);
    window.scrollTo(0, 0);
  };

  const handleFilterChange = (value: string) => {
    setFilter(value);
    setPage(1);

    // Update URL
    const newSearchParams = new URLSearchParams(searchParams);
    if (value !== "all") {
      newSearchParams.set("filter", value);
    } else {
      newSearchParams.delete("filter");
    }
    newSearchParams.set("page", "1");
    navigate(`${location.pathname}?${newSearchParams.toString()}`);
  };

  const handleSortChange = (value: string) => {
    setSort(value);
    setPage(1);

    // Update URL
    const newSearchParams = new URLSearchParams(searchParams);
    newSearchParams.set("sort", value);
    newSearchParams.set("page", "1");
    navigate(`${location.pathname}?${newSearchParams.toString()}`);
  };

  const toggleSortDirection = () => {
    const newDirection = sortDirection === "desc" ? "asc" : "desc";
    setSortDirection(newDirection);
    setPage(1);

    // Update URL
    const newSearchParams = new URLSearchParams(searchParams);
    newSearchParams.set("order", newDirection);
    newSearchParams.set("page", "1");
    navigate(`${location.pathname}?${newSearchParams.toString()}`);
  };

  const fetchAllRepositories = () => {
    navigate("/repositories?viewAll=true");
  };

  const totalPages = Math.ceil(totalCount / ITEMS_PER_PAGE);

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
      <div className="flex flex-col md:flex-row md:items-center md:justify-between mb-6">
        <h1 className="text-3xl font-bold tracking-tight mb-4 md:mb-0">
          {codeSearch
            ? "MCP Server Search Results"
            : searchQuery
            ? `Search Results: ${searchQuery}`
            : "MCP Server Repositories"}
        </h1>
        <div className="flex flex-col sm:flex-row gap-2">
          <div className="flex items-center gap-2">
            <Select value={filter} onValueChange={handleFilterChange}>
              <SelectTrigger className="w-[180px]">
                <Filter className="mr-2 h-4 w-4" />
                <SelectValue placeholder="Filter by source" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">All</SelectItem>
                <SelectItem value="Verified">Verified</SelectItem>
              </SelectContent>
            </Select>

            <Select value={sort} onValueChange={handleSortChange}>
              <SelectTrigger className="w-[180px]">
                <SelectValue placeholder="Sort by" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="stars">Stars</SelectItem>
                <SelectItem value="name">Name</SelectItem>
              </SelectContent>
            </Select>

            <Button variant="outline" size="icon" onClick={toggleSortDirection}>
              {sortDirection === "desc" ? (
                <SortDesc className="h-4 w-4" />
              ) : (
                <SortAsc className="h-4 w-4" />
              )}
            </Button>
          </div>
          <Button onClick={fetchAllRepositories} variant="outline" size="sm">
            Show All Repositories
          </Button>
        </div>
      </div>

      {isCodeSearching && (
        <Card className="mb-6">
          <CardContent className="flex items-center justify-center py-8">
            <Loader className="h-6 w-6 animate-spin mr-2" />
            <p>Searching GitHub for MCP Server repositories...</p>
          </CardContent>
        </Card>
      )}

      {repositories?.length === 0 ? (
        <Card>
          <CardContent className="flex flex-col items-center justify-center py-12">
            <p className="text-muted-foreground mb-4">No repositories found</p>
            <Button asChild variant="outline">
              <Link to="/">Back to Dashboard</Link>
            </Button>
          </CardContent>
        </Card>
      ) : (
        <>
          <div className="grid gap-6 md:grid-cols-2 lg:grid-cols-3">
            {repositories?.map((repo) => (
              <Card key={repo.id} className="overflow-hidden">
                <CardHeader className="pb-3">
                  <div className="flex justify-between items-start">
                    <CardTitle className="text-lg font-medium flex items-center">
                      {repo.icon ? (
                        <img
                          src={repo.icon}
                          alt=""
                          className="h-5 w-5 mr-2 rounded-sm"
                        />
                      ) : (
                        <Code className="h-5 w-5 mr-2 text-muted-foreground" />
                      )}
                      <Link
                        to={`/repository/${repo.id}`}
                        className="hover:underline line-clamp-1"
                      >
                        {repo.displayName}
                      </Link>
                    </CardTitle>
                    <Button
                      asChild
                      variant="ghost"
                      size="sm"
                      className="h-8 w-8 p-0"
                    >
                      <a
                        href={repo.url}
                        target="_blank"
                        rel="noopener noreferrer"
                        aria-label="View on GitHub"
                      >
                        <ExternalLink className="h-4 w-4" />
                      </a>
                    </Button>
                  </div>
                </CardHeader>
                <CardContent>
                  <p className="text-sm text-muted-foreground line-clamp-2 mb-4 h-10">
                    {repo.description || "No description available"}
                  </p>
                  <div className="flex flex-wrap gap-2 mt-2">
                    <span className="inline-flex items-center rounded-full bg-secondary px-2.5 py-0.5 text-xs font-medium">
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
                              className="inline-flex items-center rounded-full border border-primary/20 bg-primary/10 text-primary px-3 py-1 text-xs font-medium mr-2 shadow-sm hover:bg-primary/20 transition"
                              key={index}
                            >
                              {cat}
                            </span>
                          ))}
                      </>
                    )}
                  </div>
                </CardContent>
              </Card>
            ))}
          </div>

          {totalPages > 1 && (
            <div className="flex justify-center mt-8">
              <div className="flex flex-wrap justify-center gap-2">
                <Button
                  variant="outline"
                  onClick={() => handlePageChange(page - 1)}
                  disabled={page === 1}
                >
                  Previous
                </Button>

                {/* Show page numbers with ellipsis for large page counts */}
                {Array.from({ length: Math.min(totalPages, 5) }, (_, i) => {
                  // For first 5 pages, show 1-5
                  if (totalPages <= 5 || page <= 3) {
                    return i + 1;
                  }
                  // For last 5 pages, show last 5
                  if (page >= totalPages - 2) {
                    return totalPages - 4 + i;
                  }
                  // For middle pages, show current page and 2 on each side
                  return page - 2 + i;
                }).map((pageNum) => (
                  <Button
                    key={pageNum}
                    variant={pageNum === page ? "default" : "outline"}
                    onClick={() => handlePageChange(pageNum)}
                    className="w-10 h-10 p-0"
                  >
                    {pageNum}
                  </Button>
                ))}

                {/* Show ellipsis if needed */}
                {totalPages > 5 && page < totalPages - 2 && (
                  <span className="flex items-center justify-center w-10 h-10">
                    ...
                  </span>
                )}

                {/* Always show last page if there are more than 5 pages */}
                {totalPages > 5 && page < totalPages - 2 && (
                  <Button
                    variant="outline"
                    onClick={() => handlePageChange(totalPages)}
                    className="w-10 h-10 p-0"
                  >
                    {totalPages}
                  </Button>
                )}

                <Button
                  variant="outline"
                  onClick={() => handlePageChange(page + 1)}
                  disabled={page === totalPages}
                >
                  Next
                </Button>
              </div>
            </div>
          )}

          <div className="text-center text-sm text-muted-foreground mt-4">
            Showing {(page - 1) * ITEMS_PER_PAGE + 1} to{" "}
            {Math.min(page * ITEMS_PER_PAGE, totalCount)} of {totalCount}{" "}
            repositories
          </div>
        </>
      )}
    </div>
  );
};

export default Dashboard;
