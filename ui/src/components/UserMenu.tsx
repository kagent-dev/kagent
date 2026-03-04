"use client";

import { User, LogOut, ChevronDown } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { useAuth } from "@/contexts/AuthContext";

// SSO logout path - defaults to oauth2-proxy's sign_out endpoint
const SSO_LOGOUT_PATH = process.env.NEXT_PUBLIC_SSO_LOGOUT_PATH || "/oauth2/sign_out";

interface UserMenuProps {
  onMobileLinkClick?: () => void;
}

export function UserMenu({ onMobileLinkClick }: UserMenuProps) {
  const { user } = useAuth();

  // Don't render anything if no user (unsecured mode)
  if (!user) {
    return null;
  }

  const name = String(user.name || user.preferred_username || "");
  const email = String(user.email || "");
  const sub = String(user.sub || "");
  const displayName = name || email || sub;

  const handleLogout = () => {
    onMobileLinkClick?.();
    window.location.href = SSO_LOGOUT_PATH;
  };

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          variant="ghost"
          className="flex items-center gap-2 text-sm text-muted-foreground hover:text-foreground"
        >
          <User className="h-4 w-4" />
          <span className="max-w-[150px] truncate">{displayName}</span>
          <ChevronDown className="h-3 w-3" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-64">
        {/* User Info Header */}
        <DropdownMenuLabel className="font-normal">
          <div className="flex flex-col space-y-1">
            {name && (
              <p className="text-sm font-medium leading-none">{name}</p>
            )}
            {email && (
              <p className="text-xs text-muted-foreground">{email}</p>
            )}
            {!name && !email && (
              <p className="text-sm font-medium leading-none">{sub}</p>
            )}
          </div>
        </DropdownMenuLabel>

        <DropdownMenuSeparator />

        {/* Logout */}
        <DropdownMenuItem
          onClick={handleLogout}
          className="cursor-pointer text-destructive focus:text-destructive"
        >
          <LogOut className="h-4 w-4 mr-2" />
          Sign out
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
