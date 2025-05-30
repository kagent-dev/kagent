'use client'
import { useState } from "react";
import Link from "next/link";
import { Button } from "./ui/button";
import KAgentLogoWithText from "./kagent-logo-text";
import KagentLogo from "./kagent-logo";
import { Plus, Menu, X, ChevronDown, Brain, Server, Eye, Hammer, HomeIcon, Wrench, Database } from "lucide-react";
import { ThemeToggle } from "./ThemeToggle";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";

export function Header() {
  const [isMenuOpen, setIsMenuOpen] = useState(false);

  const toggleMenu = () => {
    setIsMenuOpen(!isMenuOpen);
  };

  // Close mobile menu when a link inside dropdown is clicked
  const handleMobileLinkClick = () => {
    if (isMenuOpen) {
      setIsMenuOpen(false);
    }
  };

  return (
    <nav className="py-4 md:py-8 border-b bg-background z-50 relative">
      <div className="max-w-6xl mx-auto px-4 md:px-6">
        <div className="flex justify-between items-center min-h-[3.5rem] md:min-h-[4.5rem]">
          <Link href="/">
            <KAgentLogoWithText className="h-5" />
          </Link>
          
          {/* Mobile menu button */}
          <button 
            className="md:hidden p-2 focus:outline-none z-50 relative"
            onClick={toggleMenu}
            aria-label="Toggle menu"
          >
            {isMenuOpen ? <X className="h-6 w-6" /> : <Menu className="h-6 w-6" />}
          </button>
          
          {/* Desktop navigation */}
          <div className="hidden md:flex items-center space-x-2 lg:space-x-4">
            <Button variant="link" className="text-secondary-foreground" asChild>
              <Link href="/" className="gap-1">
                <HomeIcon className="h-4 w-4" />
                Home
              </Link>
            </Button>


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
                    New Tool Server
                  </Link>
                </DropdownMenuItem>
                <DropdownMenuItem asChild>
                  <Link href="/memories/new" className="gap-2 cursor-pointer w-full">
                    <Database className="h-4 w-4" />
                    New Memory
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
                    Tool Servers
                  </Link>
                </DropdownMenuItem>
                <DropdownMenuItem asChild>
                  <Link href="/memories" className="gap-2 cursor-pointer w-full">
                    <Database className="h-4 w-4" />
                    Memory
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
          </div>
        </div>
        
        {/* Mobile menu and overlay */}
        {isMenuOpen && (
          <>
            {/* Overlay to prevent background interaction */}
            <div className="fixed inset-0 bg-black/30 z-40 md:hidden animate-in fade-in" onClick={toggleMenu} aria-hidden="true" />
            <div 
              className="md:hidden fixed left-0 right-0 top-[60px] sm:top-[72px] z-50 bg-background shadow-lg rounded-b-lg animate-in fade-in slide-in-from-top-4 duration-200 mx-auto max-w-2xl"
              role="dialog"
              aria-modal="true"
              aria-label="Mobile navigation menu"
              style={{ maxHeight: 'calc(100dvh - 60px)', overflowY: 'auto' }}
            >
              <div className="flex flex-col divide-y divide-border">
                {/* Mobile Home Link */}
                <Button variant="ghost" className="text-secondary-foreground justify-start px-4 py-3 gap-2 w-full hover:bg-accent" asChild>
                  <Link href="/" onClick={handleMobileLinkClick}>
                    <HomeIcon className="h-4 w-4" />
                    Home
                  </Link>
                </Button>

                {/* Mobile View Dropdown */}
                <DropdownMenu>
                  <DropdownMenuTrigger asChild>
                    <Button variant="ghost" className="text-secondary-foreground justify-start px-4 py-3 gap-1 w-full hover:bg-accent">
                      <Eye className="h-4 w-4" />
                      View
                      <ChevronDown className="h-4 w-4 ml-auto" />
                    </Button>
                  </DropdownMenuTrigger>
                  <DropdownMenuContent 
                    align="start" 
                    className="w-[calc(100%-2rem)] mx-4 mt-1 rounded-lg border shadow-lg"
                    sideOffset={4}
                  >
                    <DropdownMenuItem asChild onClick={handleMobileLinkClick}>
                      <Link href="/agents" className="gap-2 cursor-pointer w-full py-2">
                        <KagentLogo className="h-4 w-4 text-primary" />
                        My Agents
                      </Link>
                    </DropdownMenuItem>
                    <DropdownMenuItem asChild onClick={handleMobileLinkClick}>
                      <Link href="/models" className="gap-2 cursor-pointer w-full py-2">
                        <Brain className="h-4 w-4" />
                        Models
                      </Link>
                    </DropdownMenuItem>
                    <DropdownMenuItem asChild onClick={handleMobileLinkClick}>
                      <Link href="/tools" className="gap-2 cursor-pointer w-full py-2">
                        <Hammer className="h-4 w-4" />
                        Tools
                      </Link>
                    </DropdownMenuItem>
                    <DropdownMenuItem asChild onClick={handleMobileLinkClick}>
                      <Link href="/servers" className="gap-2 cursor-pointer w-full py-2">
                        <Server className="h-4 w-4" />
                        Tool Servers
                      </Link>
                    </DropdownMenuItem>
                    <DropdownMenuItem asChild onClick={handleMobileLinkClick}>
                      <Link href="/memories" className="gap-2 cursor-pointer w-full py-2">
                        <Database className="h-4 w-4" />
                        Memory
                      </Link>
                    </DropdownMenuItem>
                  </DropdownMenuContent>
                </DropdownMenu>

                {/* Mobile Create Dropdown */}
                <DropdownMenu>
                  <DropdownMenuTrigger asChild>
                    <Button variant="ghost" className="text-secondary-foreground justify-start px-4 py-3 gap-1 w-full hover:bg-accent">
                      <Plus className="h-4 w-4" />
                      Create
                      <ChevronDown className="h-4 w-4 ml-auto" />
                    </Button>
                  </DropdownMenuTrigger>
                  <DropdownMenuContent 
                    align="start" 
                    className="w-[calc(100%-2rem)] mx-4 mt-1 rounded-lg border shadow-lg"
                    sideOffset={4}
                  >
                    <DropdownMenuItem asChild onClick={handleMobileLinkClick}>
                      <Link href="/agents/new" className="gap-2 cursor-pointer w-full py-2">
                        <KagentLogo className="h-4 w-4 text-primary" />
                        New Agent
                      </Link>
                    </DropdownMenuItem>
                    <DropdownMenuItem asChild onClick={handleMobileLinkClick}>
                      <Link href="/models/new" className="gap-2 cursor-pointer w-full py-2">
                        <Brain className="h-4 w-4" />
                        New Model
                      </Link>
                    </DropdownMenuItem>
                    <DropdownMenuItem asChild onClick={handleMobileLinkClick}>
                      <Link href="/tools/new" className="gap-2 cursor-pointer w-full py-2">
                        <Wrench className="h-4 w-4" />
                        New Tool
                      </Link>
                    </DropdownMenuItem>
                    <DropdownMenuItem asChild onClick={handleMobileLinkClick}>
                      <Link href="/servers/new" className="gap-2 cursor-pointer w-full py-2">
                        <Server className="h-4 w-4" />
                        New Tool Server
                      </Link>
                    </DropdownMenuItem>
                    <DropdownMenuItem asChild onClick={handleMobileLinkClick}>
                      <Link href="/memories/new" className="gap-2 cursor-pointer w-full py-2">
                        <Database className="h-4 w-4" />
                        New Memory
                      </Link>
                    </DropdownMenuItem>
                  </DropdownMenuContent>
                </DropdownMenu>
                
                {/* Mobile Other Links */}
                <Button variant="ghost" className="text-secondary-foreground justify-start px-4 py-3 hover:bg-accent" asChild>
                  <Link href="https://github.com/kagent-dev/kagent" target="_blank" onClick={handleMobileLinkClick}>Contribute</Link>
                </Button>
                <Button variant="ghost" className="text-secondary-foreground justify-start px-4 py-3 hover:bg-accent" asChild>
                  <Link href="https://discord.gg/Fu3k65f2k3" target="_blank" onClick={handleMobileLinkClick}>Community</Link>
                </Button>

                <div className="flex items-center justify-end p-4">
                  <ThemeToggle />
                </div>
              </div>
            </div>
          </>
        )}
      </div>
    </nav>
  );
}