import { Link } from "react-router-dom";
import { Search, Home, List, Code, LogIn, LogOut } from "lucide-react";
import { Button } from "./ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "./ui/dialog";
import { Input } from "./ui/input";
import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import React from "react";

export function Header() {
  const [searchQuery, setSearchQuery] = useState("");
  const [showLogin, setShowLogin] = useState(false);
  const [tokenInput, setTokenInput] = useState("");
  const [hasToken, setHasToken] = useState(false);
  const navigate = useNavigate();

  useEffect(() => {
    const cookieMatch = document.cookie.match(
      /obot-catalog-server-token=([^;]+)/
    );
    setHasToken(!!cookieMatch);
  }, []);

  const handleSearch = (e: React.KeyboardEvent) => {
    if (e.key === "Enter" && searchQuery.trim()) {
      navigate(`/?q=${encodeURIComponent(searchQuery.trim())}`);
      setSearchQuery("");
    }
  };

  const handleViewAllRepos = () => {
    navigate("/repositories?viewAll=true");
  };

  const handleTokenSubmit = () => {
    if (tokenInput.trim()) {
      document.cookie = `obot-catalog-server-token=${encodeURIComponent(
        tokenInput.trim()
      )}; path=/`;
      setHasToken(true);
      setShowLogin(false);
    }
  };

  const handleLogout = () => {
    document.cookie =
      "obot-catalog-server-token=; path=/; expires=Thu, 01 Jan 1970 00:00:00 UTC;";
    setHasToken(false);
  };

  return (
    <header className="sticky top-0 z-50 w-full border-b bg-background/95 backdrop-blur supports-[backdrop-filter]:bg-background/60">
      <div className="container flex h-14 items-center">
        <Link to="/" className="flex items-center space-x-2">
          <Code className="h-6 w-6" />
          <span className="font-bold">Repository Explorer</span>
        </Link>
        <div className="flex flex-1 items-center justify-between space-x-2 md:justify-end">
          <div className="w-full flex-1 md:w-auto md:flex-none">
            <div className="relative">
              <Search className="absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
              <input
                type="search"
                placeholder="Search repositories..."
                className="w-full rounded-md border border-input bg-background py-2 pl-8 text-sm ring-offset-background file:border-0 file:bg-transparent file:text-sm file:font-medium placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50 md:w-[200px] lg:w-[300px]"
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
                onKeyDown={handleSearch}
              />
            </div>
          </div>
          <nav className="flex items-center space-x-2">
            <Button onClick={handleViewAllRepos} variant="outline" size="sm">
              <List className="mr-2 h-4 w-4" />
              View All Repos
            </Button>
            <Button asChild variant="ghost" size="sm">
              <Link to="/">
                <Home className="mr-2 h-4 w-4" />
                Dashboard
              </Link>
            </Button>
            <Button asChild variant="ghost" size="sm">
              <Link to="/repositories">
                <List className="mr-2 h-4 w-4" />
                Repositories
              </Link>
            </Button>
            {!hasToken && (
              <Button
                variant="ghost"
                size="sm"
                onClick={() => setShowLogin(true)}
              >
                <LogIn className="mr-2 h-4 w-4" />
                Login
              </Button>
            )}
            {hasToken && (
              <Button variant="ghost" size="sm" onClick={handleLogout}>
                <LogOut className="mr-2 h-4 w-4" />
                Logout
              </Button>
            )}
          </nav>
        </div>
      </div>

      <Dialog open={showLogin} onOpenChange={setShowLogin}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Enter Access Token</DialogTitle>
          </DialogHeader>
          <Input
            placeholder="obot-catalog-server-token"
            value={tokenInput}
            onChange={(e) => setTokenInput(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && handleTokenSubmit()}
          />
          <Button className="mt-4 w-full" onClick={handleTokenSubmit}>
            Submit Token
          </Button>
        </DialogContent>
      </Dialog>
    </header>
  );
}

export default Header;
