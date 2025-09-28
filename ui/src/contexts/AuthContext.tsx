"use client";

import React, { createContext, useContext, useState, useEffect, ReactNode } from "react";

export interface User {
  id: string;
  email: string;
  name: string;
  role: string;
  createdAt: string;
}

interface AuthContextType {
  user: User | null;
  isLoading: boolean;
  isAuthenticated: boolean;
  login: (email: string, password: string) => Promise<{ success: boolean; error?: string }>;
  logout: () => void;
  signup: (email: string, password: string, name: string) => Promise<{ success: boolean; error?: string }>;
}

const AuthContext = createContext<AuthContextType | undefined>(undefined);

export function useAuth() {
  const context = useContext(AuthContext);
  if (context === undefined) {
    throw new Error("useAuth must be used within an AuthProvider");
  }
  return context;
}

interface AuthProviderProps {
  children: ReactNode;
}

export function AuthProvider({ children }: AuthProviderProps) {
  const [user, setUser] = useState<User | null>(null);
  const [isLoading, setIsLoading] = useState(true);

  // Check for existing session on mount
  useEffect(() => {
    const checkAuthStatus = async () => {
      try {
        // For demo purposes, check if user is logged in
        // In a real app, this would validate a token or session
        if (typeof window !== 'undefined') {
          const savedUser = localStorage.getItem("auth-user");
          if (savedUser) {
            setUser(JSON.parse(savedUser));
            // Also restore the auth token if it exists
            const savedToken = localStorage.getItem("auth_token");
            if (!savedToken) {
              localStorage.setItem("auth_token", "demo-token-restored-" + Date.now());
            }
          }
        }
      } catch (error) {
        console.error("Error checking auth status:", error);
      } finally {
        setIsLoading(false);
      }
    };

    checkAuthStatus();
  }, []);

  const login = async (email: string, password: string) => {
    try {
      setIsLoading(true);

      // For demo purposes, accept any email/password combination
      // In a real app, this would make an API call to authenticate
      if (!email || !password) {
        return { success: false, error: "Email and password are required" };
      }

      // Simulate API call delay
      await new Promise(resolve => setTimeout(resolve, 1000));

      // Create a demo user with role based on email
      const userName = email.split("@")[0];
      const userRole = email.toLowerCase().includes('admin') ? 'admin' : 'user';
      
      const demoUser: User = {
        id: "1",
        email,
        name: userName,
        role: userRole,
        createdAt: new Date().toISOString(),
      };

      setUser(demoUser);
      if (typeof window !== 'undefined') {
        localStorage.setItem("auth-user", JSON.stringify(demoUser));
        localStorage.setItem("auth_token", "demo-token-" + Date.now()); // Demo token for API calls
      }

      return { success: true };
    } catch (error) {
      console.error("Login error:", error);
      return { success: false, error: "An error occurred during login" };
    } finally {
      setIsLoading(false);
    }
  };

  const signup = async (email: string, password: string, name: string) => {
    try {
      setIsLoading(true);

      // For demo purposes, create user immediately
      // In a real app, this would make an API call to create the user
      if (!email || !password || !name) {
        return { success: false, error: "All fields are required" };
      }

      // Simulate API call delay
      await new Promise(resolve => setTimeout(resolve, 1000));

      // Create a demo user with role based on email
      const userRole = email.toLowerCase().includes('admin') ? 'admin' : 'user';

      const newUser: User = {
        id: Date.now().toString(),
        email,
        name,
        role: userRole,
        createdAt: new Date().toISOString(),
      };

      setUser(newUser);
      if (typeof window !== 'undefined') {
        localStorage.setItem("auth-user", JSON.stringify(newUser));
        localStorage.setItem("auth_token", "demo-token-" + Date.now()); // Demo token for API calls
      }

      return { success: true };
    } catch (error) {
      console.error("Signup error:", error);
      return { success: false, error: "An error occurred during signup" };
    } finally {
      setIsLoading(false);
    }
  };

  const logout = () => {
    setUser(null);
    if (typeof window !== 'undefined') {
      localStorage.removeItem("auth-user");
      localStorage.removeItem("auth_token");
    }
  };

  const value: AuthContextType = {
    user,
    isLoading,
    isAuthenticated: !!user,
    login,
    logout,
    signup,
  };

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}
