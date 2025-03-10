'use client'
import { useState } from "react";
import Link from "next/link";
import { Button } from "./ui/button";
import KAgentLogoWithText from "./kagent-logo-text";
import { Plus, Menu, X } from "lucide-react";
import { ThemeToggle } from "./ThemeToggle";

export function Header() {
  const [isMenuOpen, setIsMenuOpen] = useState(false);

  const toggleMenu = () => {
    setIsMenuOpen(!isMenuOpen);
  };

  return (
    <nav className="py-4 md:py-8">
      <div className="max-w-4xl mx-auto px-4 md:px-6">
        <div className="flex justify-between items-center">
          <Link href="/">
            <KAgentLogoWithText className="h-5" />
          </Link>
          
          {/* Mobile menu button */}
          <button 
            className="md:hidden p-2 focus:outline-none"
            onClick={toggleMenu}
            aria-label="Toggle menu"
          >
            {isMenuOpen ? <X className="h-6 w-6" /> : <Menu className="h-6 w-6" />}
          </button>
          
          {/* Desktop navigation */}
          <div className="hidden md:flex items-center space-x-6 lg:space-x-8">
            <Button variant="link" className="text-secondary-foreground" asChild>
              <Link href="/agents">My Agents</Link>
            </Button>
            <Button variant="link" className="text-secondary-foreground" asChild>
              <Link href="https://github.com/kagent-dev/kagent" target="_blank">Contribute</Link>
            </Button>
            <Button variant="link" className="text-secondary-foreground" asChild>
              <Link href="https://discord.gg/Fu3k65f2k3">Join the community</Link>
            </Button>
            <div>
              <ThemeToggle />
            </div>
            <Button variant="default" asChild>
              <Link href="/agents/new">
                <Plus className="h-4 w-4 mr-2" />
                New Agent
              </Link>
            </Button>
          </div>
        </div>
        
        {/* Mobile menu */}
        {isMenuOpen && (
          <div className="md:hidden pt-4 pb-2 animate-in fade-in slide-in-from-top duration-300">
            <div className="flex flex-col space-y-3">
              <Button variant="link" className="text-secondary-foreground justify-start px-1" asChild>
                <Link href="/agents">My Agents</Link>
              </Button>
              <Button variant="link" className="text-secondary-foreground justify-start px-1" asChild>
                <Link href="https://github.com/kagent-dev/kagent" target="_blank">Contribute</Link>
              </Button>
              <Button variant="link" className="text-secondary-foreground justify-start px-1" asChild>
                <Link href="https://discord.gg/Fu3k65f2k3">Join the community</Link>
              </Button>
              <div className="flex items-center justify-between py-2">
                <Button variant="default" size="sm" asChild>
                  <Link href="/agents/new">
                    <Plus className="h-4 w-4 mr-2" />
                    New Agent
                  </Link>
                </Button>
                <ThemeToggle />
              </div>
            </div>
          </div>
        )}
      </div>
    </nav>
  );
}