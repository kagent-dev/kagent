"use client";

import { User, LogOut, ChevronDown, Users } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuGroup,
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

  const displayName = user.name || user.email || user.user;

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
            {user.name && (
              <p className="text-sm font-medium leading-none">{user.name}</p>
            )}
            {user.email && (
              <p className="text-xs text-muted-foreground">{user.email}</p>
            )}
            {!user.name && !user.email && (
              <p className="text-sm font-medium leading-none">{user.user}</p>
            )}
          </div>
        </DropdownMenuLabel>

        <DropdownMenuSeparator />

        {/* Groups Section */}
        {user.groups && user.groups.length > 0 && (
          <>
            <DropdownMenuGroup>
              <DropdownMenuLabel className="flex items-center gap-2 text-xs text-muted-foreground">
                <Users className="h-3 w-3" />
                Groups
              </DropdownMenuLabel>
              <div className="px-2 py-1.5 max-h-32 overflow-y-auto">
                {user.groups.map((group, index) => (
                  <div
                    key={index}
                    className="text-xs text-muted-foreground py-0.5 pl-5 truncate"
                    title={group}
                  >
                    {group}
                  </div>
                ))}
              </div>
            </DropdownMenuGroup>
            <DropdownMenuSeparator />
          </>
        )}

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
