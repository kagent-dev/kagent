'use client';

import React, { createContext, useContext, useState, useEffect, ReactNode } from 'react';
import { useAuth } from '@/hooks/useAuth';

interface OrganizationContextType {
  currentOrganization: Organization | null;
  organizations: Organization[];
  setCurrentOrganization: (org: Organization | null) => void;
  switchOrganization: (orgId: string) => Promise<void>;
  loading: boolean;
  refreshOrganizations: () => Promise<void>;
}

interface Organization {
  id: string;
  name: string;
  domain: string;
  description?: string;
  role: 'admin' | 'user';
  status: 'active' | 'inactive' | 'pending';
}

const OrganizationContext = createContext<OrganizationContextType | undefined>(undefined);

export function OrganizationProvider({ children }: { children: ReactNode }) {
  const { user, isAuthenticated } = useAuth();
  const [currentOrganization, setCurrentOrganization] = useState<Organization | null>(null);
  const [organizations, setOrganizations] = useState<Organization[]>([]);
  const [loading, setLoading] = useState(false);
  const refreshOrganizations = async () => {

    try {
      setLoading(true);
      const token = localStorage.getItem('auth_token');
      if (!token) {
        throw new Error('No authentication token found');
      }

      const res = await fetch('/api/user/organizations', {
        headers: { 'Authorization': `Bearer ${token}` }
      });

      if (res.ok) {
        const data = await res.json();
        setOrganizations(data.organizations || []);

        // Set current organization (first one or stored preference)
        if (data.organizations?.length > 0) {
          const storedOrgId = localStorage.getItem('currentOrganizationId');
          const currentOrg = storedOrgId
            ? data.organizations.find((org: Organization) => org.id === storedOrgId)
            : data.organizations[0];

          if (currentOrg) {
            setCurrentOrganization(currentOrg);
          }
        }
      }
    } catch (error) {
      console.error('Failed to load organizations:', error);
    } finally {
      setLoading(false);
    }
  };

  // Switch to a different organization
  const switchOrganization = async (orgId: string) => {
    if (!organizations.length) return;

    const org = organizations.find(o => o.id === orgId);
    if (org) {
      setCurrentOrganization(org);
      localStorage.setItem('currentOrganizationId', orgId);

      // In a real app, you might want to reload data for the new organization
      // or trigger a page refresh
    }
  };

  useEffect(() => {
    if (isAuthenticated && user) {
      refreshOrganizations();
    } else {
      setCurrentOrganization(null);
      setOrganizations([]);
    }
  }, [isAuthenticated, user]);

  const value: OrganizationContextType = {
    currentOrganization,
    organizations,
    setCurrentOrganization,
    switchOrganization,
    loading,
    refreshOrganizations
  };

  return (
    <OrganizationContext.Provider value={value}>
      {children}
    </OrganizationContext.Provider>
  );
}

export function useOrganization() {
  const context = useContext(OrganizationContext);
  if (context === undefined) {
    throw new Error('useOrganization must be used within an OrganizationProvider');
  }
  return context;
}
