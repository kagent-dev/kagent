'use client';

import { useState, useEffect } from 'react';
import { useRouter } from 'next/navigation';
import { useAuth } from '@/hooks/useAuth';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Alert, AlertDescription } from '@/components/ui/alert';
import {
  Building2,
  Users,
  Plus,
  Settings,
  Shield,
  Crown,
  User,
  Mail,
  Calendar,
  Edit,
  Trash2,
  AlertCircle,
  CheckCircle,
  TrendingUp,
  Globe
} from 'lucide-react';
import { toast } from 'sonner';

interface Organization {
  id: string;
  name: string;
  domain: string;
  description?: string;
  adminCount: number;
  userCount: number;
  createdAt: string;
  status: 'active' | 'inactive' | 'pending';
}

interface OrganizationStats {
  total: number;
  active: number;
  pending: number;
  totalUsers: number;
}

export default function AdminOrganizationsPage() {
  const { isAuthenticated, user } = useAuth();
  const router = useRouter();
  const [organizations, setOrganizations] = useState<Organization[]>([]);
  const [loading, setLoading] = useState(true);
  const [stats, setStats] = useState<OrganizationStats>({
    total: 0,
    active: 0,
    pending: 0,
    totalUsers: 0
  });

  useEffect(() => {
    if (!isAuthenticated) return;
    if (user?.role !== 'admin') return;
    loadOrganizations();
  }, [isAuthenticated, user]);

  const loadOrganizations = async () => {
    try {
      setLoading(true);
      const token = localStorage.getItem('auth_token');
      if (!token) {
        throw new Error('No authentication token found');
      }

      const res = await fetch('/api/admin/organizations', {
        headers: { 'Authorization': `Bearer ${token}` }
      });

      if (!res.ok) {
        const errorText = await res.text();
        throw new Error(`Failed to load organizations: ${res.status} ${errorText}`);
      }

      const data = await res.json();
      setOrganizations(data.organizations || []);

      // Calculate stats
      const total = data.organizations?.length || 0;
      const active = data.organizations?.filter((org: Organization) => org.status === 'active').length || 0;
      const pending = data.organizations?.filter((org: Organization) => org.status === 'pending').length || 0;
      const totalUsers = data.organizations?.reduce((sum: number, org: Organization) => sum + org.userCount, 0) || 0;

      setStats({ total, active, pending, totalUsers });
    } catch (e) {
      console.error('Error loading organizations:', e);
      toast.error('Failed to load organizations');
    } finally {
      setLoading(false);
    }
  };

  const handleDelete = async (id: string, name: string) => {
    if (!confirm(`Delete organization "${name}"? This will remove all associated users and data.`)) return;
    try {
      const res = await fetch(`/api/admin/organizations/${id}`, {
        method: 'DELETE',
        headers: { 'Authorization': `Bearer ${localStorage.getItem('auth_token')}` }
      });
      if (!res.ok) throw new Error('Failed to delete organization');
      setOrganizations(prev => prev.filter(org => org.id !== id));
      toast.success('Organization deleted successfully');
      loadOrganizations(); // Refresh stats
    } catch (e) {
      toast.error('Failed to delete organization');
    }
  };

  const getStatusBadge = (status: string) => {
    switch (status) {
      case 'active':
        return <Badge className="bg-green-100 text-green-800 hover:bg-green-200"><CheckCircle className="w-3 h-3 mr-1" />Active</Badge>;
      case 'pending':
        return <Badge className="bg-yellow-100 text-yellow-800 hover:bg-yellow-200"><AlertCircle className="w-3 h-3 mr-1" />Pending</Badge>;
      case 'inactive':
        return <Badge variant="secondary">Inactive</Badge>;
      default:
        return <Badge variant="secondary">{status}</Badge>;
    }
  };

  const formatDate = (dateString: string) => {
    return new Date(dateString).toLocaleDateString('en-US', {
      year: 'numeric',
      month: 'short',
      day: 'numeric'
    });
  };

  if (!isAuthenticated) {
    return (
      <div className="min-h-screen bg-gradient-to-br from-background via-background to-muted/20 flex items-center justify-center">
        <div className="text-center">
          <AlertCircle className="w-16 h-16 text-muted-foreground mx-auto mb-4" />
          <h1 className="text-2xl font-bold mb-2">Authentication Required</h1>
          <p className="text-muted-foreground">Please log in to access organization management.</p>
        </div>
      </div>
    );
  }

  if (user?.role !== 'admin') {
    return (
      <div className="min-h-screen bg-gradient-to-br from-background via-background to-muted/20 flex items-center justify-center">
        <div className="text-center">
          <Shield className="w-16 h-16 text-muted-foreground mx-auto mb-4" />
          <h1 className="text-2xl font-bold mb-2">Access Denied</h1>
          <p className="text-muted-foreground">Admin privileges required to manage organizations.</p>
        </div>
      </div>
    );
  }

  return (
    <div className="min-h-screen bg-gradient-to-br from-background via-background to-muted/20">
      <div className="container mx-auto px-4 py-8">
        <div className="max-w-7xl mx-auto space-y-8">
          {/* Header */}
          <div className="flex flex-col md:flex-row md:items-center md:justify-between gap-4">
            <div>
              <div className="inline-flex items-center gap-2 px-4 py-2 rounded-full bg-gradient-to-r from-blue-100 to-purple-100 text-blue-800 text-sm font-medium mb-4">
                <Building2 className="w-4 h-4" />
                Organization Management
              </div>
              <h1 className="text-4xl font-bold bg-gradient-to-r from-slate-900 to-slate-700 bg-clip-text text-transparent">
                Organizations
              </h1>
              <p className="text-slate-600 mt-2">
                Manage multi-tenant organizations and their user access
              </p>
            </div>

            <div className="flex gap-3">
              <Button variant="outline" className="border-blue-200 hover:bg-blue-50">
                <Settings className="w-4 h-4 mr-2" />
                Settings
              </Button>
              <Button className="bg-gradient-to-r from-blue-600 to-purple-600 hover:from-blue-700 hover:to-purple-700" onClick={() => router.push('/admin/organizations/onboard')}>
                <Plus className="w-4 h-4 mr-2" />
                New Organization
              </Button>
            </div>
          </div>

          {/* Stats Cards */}
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-6">
            <Card className="group hover:shadow-lg transition-all duration-300 border-0 bg-gradient-to-br from-card to-card/50 hover:from-blue-500/5 hover:to-blue-500/10">
              <CardHeader className="pb-3">
                <div className="flex items-center gap-3">
                  <div className="w-12 h-12 rounded-xl bg-blue-500/10 flex items-center justify-center group-hover:bg-blue-500/20 transition-colors">
                    <Building2 className="w-6 h-6 text-blue-600" />
                  </div>
                  <div>
                    <CardTitle className="text-lg">Total Organizations</CardTitle>
                    <div className="text-2xl font-bold text-blue-600">{stats.total}</div>
                  </div>
                </div>
              </CardHeader>
              <CardContent>
                <p className="text-sm text-muted-foreground">All organizations</p>
              </CardContent>
            </Card>

            <Card className="group hover:shadow-lg transition-all duration-300 border-0 bg-gradient-to-br from-card to-card/50 hover:from-green-500/5 hover:to-green-500/10">
              <CardHeader className="pb-3">
                <div className="flex items-center gap-3">
                  <div className="w-12 h-12 rounded-xl bg-green-500/10 flex items-center justify-center group-hover:bg-green-500/20 transition-colors">
                    <CheckCircle className="w-6 h-6 text-green-600" />
                  </div>
                  <div>
                    <CardTitle className="text-lg">Active Organizations</CardTitle>
                    <div className="text-2xl font-bold text-green-600">{stats.active}</div>
                  </div>
                </div>
              </CardHeader>
              <CardContent>
                <p className="text-sm text-muted-foreground">Currently active</p>
              </CardContent>
            </Card>

            <Card className="group hover:shadow-lg transition-all duration-300 border-0 bg-gradient-to-br from-card to-card/50 hover:from-yellow-500/5 hover:to-yellow-500/10">
              <CardHeader className="pb-3">
                <div className="flex items-center gap-3">
                  <div className="w-12 h-12 rounded-xl bg-yellow-500/10 flex items-center justify-center group-hover:bg-yellow-500/20 transition-colors">
                    <AlertCircle className="w-6 h-6 text-yellow-600" />
                  </div>
                  <div>
                    <CardTitle className="text-lg">Pending Setup</CardTitle>
                    <div className="text-2xl font-bold text-yellow-600">{stats.pending}</div>
                  </div>
                </div>
              </CardHeader>
              <CardContent>
                <p className="text-sm text-muted-foreground">Awaiting activation</p>
              </CardContent>
            </Card>

            <Card className="group hover:shadow-lg transition-all duration-300 border-0 bg-gradient-to-br from-card to-card/50 hover:from-purple-500/5 hover:to-purple-500/10">
              <CardHeader className="pb-3">
                <div className="flex items-center gap-3">
                  <div className="w-12 h-12 rounded-xl bg-purple-500/10 flex items-center justify-center group-hover:bg-purple-500/20 transition-colors">
                    <Users className="w-6 h-6 text-purple-600" />
                  </div>
                  <div>
                    <CardTitle className="text-lg">Total Users</CardTitle>
                    <div className="text-2xl font-bold text-purple-600">{stats.totalUsers}</div>
                  </div>
                </div>
              </CardHeader>
              <CardContent>
                <p className="text-sm text-muted-foreground">Across all organizations</p>
              </CardContent>
            </Card>
          </div>

          {/* Organizations List */}
          <Card className="border-0 shadow-xl bg-white/80 backdrop-blur-sm">
            <CardHeader>
              <CardTitle className="flex items-center gap-2">
                <Building2 className="w-6 h-6 text-blue-600" />
                Organization Directory
              </CardTitle>
              <CardDescription>
                Manage and monitor all organizations in your system
              </CardDescription>
            </CardHeader>
            <CardContent>
              {loading ? (
                <div className="flex items-center justify-center py-12">
                  <div className="animate-spin rounded-full h-8 w-8 border-t-2 border-b-2 border-primary"></div>
                  <span className="ml-3 text-muted-foreground">Loading organizations...</span>
                </div>
              ) : organizations.length === 0 ? (
                <div className="text-center py-12">
                  <Building2 className="w-16 h-16 text-muted-foreground mx-auto mb-4" />
                  <h3 className="text-lg font-medium text-slate-900 mb-2">No organizations found</h3>
                  <p className="text-muted-foreground mb-4">Get started by creating your first organization.</p>
                  <Button onClick={() => router.push('/admin/organizations/onboard')}>
                    <Plus className="w-4 h-4 mr-2" />
                    Create First Organization
                  </Button>
                </div>
              ) : (
                <div className="space-y-4">
                  {organizations.map((org) => (
                    <div key={org.id} className="group border border-slate-200 rounded-lg p-6 hover:shadow-md hover:border-blue-300 transition-all">
                      <div className="flex items-center justify-between">
                        <div className="flex items-center gap-4">
                          <div className="w-12 h-12 rounded-xl bg-gradient-to-br from-blue-500 to-purple-600 flex items-center justify-center text-white font-semibold text-lg">
                            {org.name.charAt(0).toUpperCase()}
                          </div>

                          <div className="space-y-1">
                            <div className="flex items-center gap-2">
                              <h3 className="font-semibold text-slate-900">{org.name}</h3>
                              {getStatusBadge(org.status)}
                            </div>
                            <div className="flex items-center gap-4 text-sm text-slate-600">
                              <div className="flex items-center gap-1">
                                <Globe className="w-4 h-4" />
                                {org.domain}
                              </div>
                              <div className="flex items-center gap-1">
                                <Users className="w-4 h-4" />
                                {org.userCount} users
                              </div>
                              <div className="flex items-center gap-1">
                                <Calendar className="w-4 h-4" />
                                {formatDate(org.createdAt)}
                              </div>
                            </div>
                            {org.description && (
                              <p className="text-sm text-slate-600 max-w-md">{org.description}</p>
                            )}
                          </div>
                        </div>

                        <div className="flex items-center gap-2">
                          <Button asChild variant="outline" size="sm" className="hover:bg-blue-50 hover:border-blue-300">
                            <button onClick={() => router.push(`/admin/organizations/${org.id}/users`)}>
                              <Users className="w-4 h-4 mr-2" />
                              Manage Users
                            </button>
                          </Button>
                          <Button asChild variant="outline" size="sm" className="hover:bg-blue-50 hover:border-blue-300">
                            <button onClick={() => router.push(`/admin/organizations/${org.id}/settings`)}>
                              <Settings className="w-4 h-4 mr-2" />
                              Settings
                            </button>
                          </Button>
                          <Button
                            variant="destructive"
                            size="sm"
                            onClick={() => handleDelete(org.id, org.name)}
                            className="hover:bg-red-600"
                          >
                            <Trash2 className="w-4 h-4" />
                          </Button>
                        </div>
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </CardContent>
          </Card>
        </div>
      </div>
    </div>
  );
}
