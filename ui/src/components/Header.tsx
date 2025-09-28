"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import Link from "next/link";
import AdolpheLogo from "./AdolpheLogo";
import { Button } from "./ui/button";
<<<<<<< HEAD
import KAgentLogoWithText from "./kagent-logo-text";
import KagentLogo from "./kagent-logo";
import { Plus, Menu, X, ChevronDown, Brain, Server, Eye, Hammer, HomeIcon } from "lucide-react";
=======
import { Menu, X, LogOut, User } from "lucide-react";
>>>>>>> 6c8473e (updated enterprise grade)
import { ThemeToggle } from "./ThemeToggle";
import { useAuth } from "@/hooks/useAuth";

export function Header() {
  const [isMenuOpen, setIsMenuOpen] = useState(false);
  const { isAuthenticated, user, logout } = useAuth();
  const router = useRouter();
  const homeHref = '/';

  const toggleMenu = () => {
    setIsMenuOpen(!isMenuOpen);
  };

  const handleLogout = () => {
    logout();
    setIsMenuOpen(false);
    // Redirect to home page after logout
    router.push('/');
  };

  return (
    <nav className="py-4 md:py-8 border-b">
      <div className="max-w-6xl mx-auto px-6">
        <div className="flex justify-between items-center">
          <Link href={homeHref}>
            <span className="inline-flex items-center gap-3 text-xl md:text-2xl font-semibold tracking-tight hover:opacity-80 transition-opacity">
              <div className="p-1 rounded-lg bg-gradient-to-br from-primary/10 to-primary/5 border border-primary/20">
                <AdolpheLogo className="h-7 w-7 md:h-8 md:w-8" />
              </div>
              <span className="bg-gradient-to-r from-foreground to-muted-foreground bg-clip-text text-transparent">
                adolphe.ai
              </span>
            </span>
          </Link>

          {/* Desktop Navigation */}
          <div className="hidden md:flex items-center gap-4">
            {isAuthenticated ? (
              <div className="flex items-center gap-4">
                <div className="flex items-center gap-2 text-sm">
                  <User className="h-4 w-4" />
                  <span>Welcome, {user?.name || user?.email}</span>
                </div>
                <Button variant="outline" size="sm" onClick={handleLogout}>
                  <LogOut className="h-4 w-4 mr-2" />
                  Logout
                </Button>
                <ThemeToggle />
              </div>
            ) : (
              <div className="flex items-center gap-4">
                <Button variant="ghost" asChild>
                  <Link href="/login">Log in</Link>
                </Button>
                <Button asChild>
                  <Link href="/signup">Sign up</Link>
                </Button>
                <ThemeToggle />
              </div>
            )}
          </div>

          {/* Mobile menu button */}
          <div className="md:hidden">
            <Button
              variant="ghost"
              size="icon"
              onClick={toggleMenu}
              aria-label={isMenuOpen ? 'Close menu' : 'Open menu'}
            >
              {isMenuOpen ? <X className="h-5 w-5" /> : <Menu className="h-5 w-5" />}
            </Button>
<<<<<<< HEAD


            {/* Create Dropdown */}
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="link" className="text-secondary-foreground gap-1 px-2">
                  <Plus className="h-4 w-4" />
                  Create
                  <ChevronDown className="h-4 w-4" />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-48">
                <DropdownMenuItem asChild>
                  <Link href="/agents/new" className="gap-2 cursor-pointer w-full">
                    <KagentLogo className="h-4 w-4 text-primary" />
                    New Agent
                  </Link>
                </DropdownMenuItem>
                <DropdownMenuItem asChild>
                  <Link href="/models/new" className="gap-2 cursor-pointer w-full">
                    <Brain className="h-4 w-4" />
                    New Model
                  </Link>
                </DropdownMenuItem>
                <DropdownMenuItem asChild>
                  <Link href="/servers" className="gap-2 cursor-pointer w-full">
                    <Server className="h-4 w-4" />
                    New MCP Server
                  </Link>
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
            
            {/* View Dropdown */}
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="link" className="text-secondary-foreground gap-1 px-2">
                  View
                  <ChevronDown className="h-4 w-4" />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-48">
                <DropdownMenuItem asChild>
                  <Link href="/agents" className="gap-2 cursor-pointer w-full">
                    <KagentLogo className="h-4 w-4 text-primary" />
                    My Agents
                  </Link>
                </DropdownMenuItem>
                <DropdownMenuItem asChild>
                  <Link href="/models" className="gap-2 cursor-pointer w-full">
                    <Brain className="h-4 w-4" />
                    Models
                  </Link>
                </DropdownMenuItem>
                <DropdownMenuItem asChild>
                  <Link href="/tools" className="gap-2 cursor-pointer w-full">
                    <Hammer className="h-4 w-4" />
                    Tools
                  </Link>
                </DropdownMenuItem>
                <DropdownMenuItem asChild>
                  <Link href="/servers" className="gap-2 cursor-pointer w-full">
                    <Server className="h-4 w-4" />
                    MCP Servers
                  </Link>
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>


            {/* Other Links */}
            <Button variant="link" className="text-secondary-foreground" asChild>
              <Link href="https://github.com/kagent-dev/kagent" target="_blank">Contribute</Link>
            </Button>
            <Button variant="link" className="text-secondary-foreground" asChild>
              <Link href="https://discord.gg/Fu3k65f2k3" target="_blank">Community</Link>
            </Button>
            
            <ThemeToggle />
=======
>>>>>>> 6c8473e (updated enterprise grade)
          </div>
        </div>

        {/* Mobile menu */}
        {isMenuOpen && (
<<<<<<< HEAD
          <div className="md:hidden pt-4 pb-2 animate-in fade-in slide-in-from-top duration-300">
            <div className="flex flex-col space-y-1">
              {/* Mobile Home Link */}
              <Button variant="ghost" className="text-secondary-foreground justify-start px-1 gap-2" asChild>
                <Link href="/" onClick={handleMobileLinkClick}>
                  <HomeIcon className="h-4 w-4" />
                  Home
                </Link>
              </Button>

              {/* Mobile View Dropdown */}
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <Button variant="ghost" className="text-secondary-foreground justify-start px-1 gap-1 w-full">
                    <Eye className="h-4 w-4" />
                    View
                    <ChevronDown className="h-4 w-4" />
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="start" className="w-56">
                  <DropdownMenuItem asChild onClick={handleMobileLinkClick}>
                    <Link href="/agents" className="gap-2 cursor-pointer w-full">
                      <KagentLogo className="h-4 w-4 text-primary" />
                      My Agents
                    </Link>
                  </DropdownMenuItem>
                  <DropdownMenuItem asChild onClick={handleMobileLinkClick}>
                    <Link href="/models" className="gap-2 cursor-pointer w-full">
                      <Brain className="h-4 w-4" />
                      Models
                    </Link>
                  </DropdownMenuItem>
                  <DropdownMenuItem asChild onClick={handleMobileLinkClick}>
                    <Link href="/tools" className="gap-2 cursor-pointer w-full">
                      <Hammer className="h-4 w-4" />
                      MCP Tools
                    </Link>
                  </DropdownMenuItem>
                  <DropdownMenuItem asChild onClick={handleMobileLinkClick}>
                    <Link href="/servers" className="gap-2 cursor-pointer w-full">
                      <Server className="h-4 w-4" />
                      MCP Servers
                    </Link>
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>

              {/* Mobile Create Dropdown */}
               <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <Button variant="ghost" className="text-secondary-foreground justify-start px-1 gap-1 w-full">
                     <Plus className="h-4 w-4" />
                    Create
                    <ChevronDown className="h-4 w-4" />
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="start" className="w-56">
                   <DropdownMenuItem asChild onClick={handleMobileLinkClick}>
                    <Link href="/agents/new" className="gap-2 cursor-pointer w-full">
                      <KagentLogo className="h-4 w-4 text-primary" />
                      New Agent
                    </Link>
                  </DropdownMenuItem>
                  <DropdownMenuItem asChild onClick={handleMobileLinkClick}>
                    <Link href="/models/new" className="gap-2 cursor-pointer w-full">
                      <Brain className="h-4 w-4" />
                      New Model
                    </Link>
                  </DropdownMenuItem>
                  <DropdownMenuItem asChild onClick={handleMobileLinkClick}>
                    <Link href="/servers/new" className="gap-2 cursor-pointer w-full">
                      <Server className="h-4 w-4" />
                      New MCP Server
                    </Link>
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
              
              {/* Mobile Other Links */}
              <Button variant="ghost" className="text-secondary-foreground justify-start px-1" asChild>
                <Link href="https://github.com/kagent-dev/kagent" target="_blank" onClick={handleMobileLinkClick}>Contribute</Link>
              </Button>
              <Button variant="ghost" className="text-secondary-foreground justify-start px-1" asChild>
                <Link href="https://discord.gg/Fu3k65f2k3" target="_blank" onClick={handleMobileLinkClick}>Community</Link>
              </Button>

              <div className="flex items-center justify-end py-2">
                 <ThemeToggle />
=======
          <div className="md:hidden mt-4 pb-4 space-y-4">
            {isAuthenticated ? (
              <div className="space-y-4">
                <div className="flex items-center gap-2 text-sm px-2">
                  <User className="h-4 w-4" />
                  <span>Welcome, {user?.name || user?.email}</span>
                </div>
                <Button variant="outline" onClick={handleLogout} className="w-full">
                  <LogOut className="h-4 w-4 mr-2" />
                  Logout
                </Button>
                <div className="pt-2 border-t">
                  <ThemeToggle />
                </div>
>>>>>>> 6c8473e (updated enterprise grade)
              </div>
            ) : (
              <div className="space-y-4">
                <Button variant="ghost" asChild className="w-full">
                  <Link href="/login" onClick={() => setIsMenuOpen(false)}>Log in</Link>
                </Button>
                <Button asChild className="w-full">
                  <Link href="/signup" onClick={() => setIsMenuOpen(false)}>Sign up</Link>
                </Button>
                <div className="pt-2 border-t">
                  <ThemeToggle />
                </div>
              </div>
            )}
          </div>
        )}
      </div>
    </nav>
  );
}
