'use client';

import { useEffect, useState } from 'react';
import { useParams, useRouter } from 'next/navigation';
import { useAuth } from '@/hooks/useAuth';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from '@/components/ui/card';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Badge } from '@/components/ui/badge';
import { Alert, AlertDescription } from '@/components/ui/alert';
import {
  ArrowLeft,
  Save,
  Loader2,
  User,
  Mail,
  Shield,
  Building2,
  AlertCircle
} from 'lucide-react';
import { toast } from 'sonner';

interface UserForm {
  name: string;
  email: string;
  role: string;
  organizationId?: string;
}

interface Organization {
  id: string;
  name: string;
  domain: string;
  status: 'active' | 'inactive' | 'pending';
}

interface UserData {
  id: string;
  name: string;
  email: string;
  role: string;
  organizationId?: string;
  organization?: {
    id: string;
    name: string;
    domain: string;
  };
}

export default function EditUserPage() {
  const { isAuthenticated, user } = useAuth();
  const router = useRouter();
  const params = useParams<{ id: string }>();
  const [loading, setLoading] = useState(true);
  const [submitting, setSubmitting] = useState(false);
  const [organizations, setOrganizations] = useState<Organization[]>([]);
  const [userData, setUserData] = useState<UserData | null>(null);
  const [form, setForm] = useState<UserForm>({ name: '', email: '', role: 'user' });

  useEffect(() => {
    if (!params?.id) return;
    loadData();
  }, [params?.id]);

  const loadData = async () => {
    try {
      setLoading(true);

      // Load organizations first
      const orgRes = await fetch('/api/admin/organizations', {
        headers: { 'Authorization': `Bearer ${localStorage.getItem('auth_token')}` }
      });
      if (orgRes.ok) {
        const orgData = await orgRes.json();
        setOrganizations(orgData.organizations || []);
      }

      // Load user data
      const userRes = await fetch(`/api/admin/users/${params.id}`, {
        headers: { 'Authorization': `Bearer ${localStorage.getItem('auth_token')}` }
      });
      if (!userRes.ok) throw new Error('User not found');
      const userDataRes = await userRes.json();

      // Get organization info if user has one
      let organization = null;
      if (userDataRes.organizationId) {
        const orgRes = await fetch('/api/admin/organizations', {
          headers: { 'Authorization': `Bearer ${localStorage.getItem('auth_token')}` }
        });
        if (orgRes.ok) {
          const orgData = await orgRes.json();
          organization = orgData.organizations?.find((org: any) => org.id === userDataRes.organizationId);
        }
      }

      setUserData({ ...userDataRes, organization });
      setForm({
        name: userDataRes.name,
        email: userDataRes.email,
        role: userDataRes.role,
        organizationId: userDataRes.organizationId || undefined
      });
    } catch (e) {
      toast.error('Failed to load user data');
      console.error('Error loading data:', e);
    } finally {
      setLoading(false);
    }
  };

  if (!isAuthenticated) {
    return (
      <div className="min-h-screen bg-gradient-to-br from-background via-background to-muted/20 flex items-center justify-center">
        <div className="text-center">
          <AlertCircle className="w-16 h-16 text-muted-foreground mx-auto mb-4" />
          <h1 className="text-2xl font-bold mb-2">Authentication Required</h1>
          <p className="text-muted-foreground">Please log in to access user management.</p>
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
          <p className="text-muted-foreground">Admin privileges required to edit users.</p>
        </div>
      </div>
    );
  }

  const onSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setSubmitting(true);
    try {
      const res = await fetch(`/api/admin/users/${params.id}`, {
        method: 'PUT',
        headers: {
          'Content-Type': 'application/json',
          'Authorization': `Bearer ${localStorage.getItem('auth_token')}`
        },
        body: JSON.stringify(form),
      });
      if (!res.ok) {
        const data = await res.json().catch(() => ({}));
        throw new Error(data?.error || 'Failed to update user');
      }
      toast.success('User updated successfully');
      router.push('/admin/users');
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'Failed to update user');
    } finally {
      setSubmitting(false);
    }
  };

  const getRoleBadge = (role: string) => {
    switch (role) {
      case 'admin':
        return <Badge className="bg-red-100 text-red-800 hover:bg-red-200"><Shield className="w-3 h-3 mr-1" />Admin</Badge>;
      case 'user':
        return <Badge className="bg-blue-100 text-blue-800 hover:bg-blue-200"><User className="w-3 h-3 mr-1" />User</Badge>;
      default:
        return <Badge variant="secondary">{role}</Badge>;
    }
  };

  const getOrgBadge = (status: string) => {
    switch (status) {
      case 'active':
        return <Badge className="bg-green-100 text-green-800 hover:bg-green-200">Active</Badge>;
      case 'pending':
        return <Badge className="bg-yellow-100 text-yellow-800 hover:bg-yellow-200">Pending</Badge>;
      case 'inactive':
        return <Badge variant="secondary">Inactive</Badge>;
      default:
        return <Badge variant="secondary">{status}</Badge>;
    }
  };

  return (
    <div className="min-h-screen bg-gradient-to-br from-background via-background to-muted/20">
      <div className="container mx-auto px-4 py-8">
        <div className="max-w-2xl mx-auto">
          {/* Header */}
          <div className="flex items-center gap-4 mb-8">
            <Button
              variant="outline"
              size="sm"
              onClick={() => router.push('/admin/users')}
              className="border-slate-200 hover:bg-slate-50"
            >
              <ArrowLeft className="w-4 h-4 mr-2" />
              Back to Users
            </Button>
            <div>
              <h1 className="text-3xl font-bold bg-gradient-to-r from-slate-900 to-slate-700 bg-clip-text text-transparent">
                Edit User
              </h1>
              <p className="text-slate-600 mt-1">
                Update user account details and organization assignment
              </p>
            </div>
          </div>

          <Card className="border-0 shadow-xl bg-white/80 backdrop-blur-sm">
            <CardHeader>
              <CardTitle className="flex items-center gap-2">
                <User className="w-6 h-6 text-blue-600" />
                User Information
              </CardTitle>
              <CardDescription>
                Modify user details, role, and organization assignment
              </CardDescription>
            </CardHeader>

            {loading ? (
              <CardContent>
                <div className="flex items-center justify-center py-12">
                  <div className="animate-spin rounded-full h-8 w-8 border-t-2 border-b-2 border-primary"></div>
                  <span className="ml-3 text-muted-foreground">Loading user data...</span>
                </div>
              </CardContent>
            ) : (
              <form onSubmit={onSubmit}>
                <CardContent className="space-y-6">
                  {/* Current User Info */}
                  {userData && (
                    <div className="p-4 bg-slate-50 rounded-lg border">
                      <h3 className="font-semibold text-slate-900 mb-3">Current Information</h3>
                      <div className="grid grid-cols-2 gap-4 text-sm">
                        <div>
                          <span className="text-slate-600">Current Role:</span>
                          <div className="mt-1">{getRoleBadge(userData.role)}</div>
                        </div>
                        <div>
                          <span className="text-slate-600">Current Organization:</span>
                          <div className="mt-1">
                            {userData.organization ? (
                              <div className="flex items-center gap-2">
                                <span className="font-medium">{userData.organization.name}</span>
                                {getOrgBadge(userData.organization.domain)}
                              </div>
                            ) : (
                              <span className="text-slate-500">No organization assigned</span>
                            )}
                          </div>
                        </div>
                      </div>
                    </div>
                  )}

                  {/* Name Field */}
                  <div className="space-y-2">
                    <Label htmlFor="name" className="text-sm font-medium text-slate-700">
                      Full Name
                    </Label>
                    <Input
                      id="name"
                      value={form.name}
                      onChange={(e) => setForm({ ...form, name: e.target.value })}
                      placeholder="Enter user's full name"
                      required
                      className="h-12 border-slate-200 focus:border-blue-500 focus:ring-blue-500"
                    />
                  </div>

                  {/* Email Field */}
                  <div className="space-y-2">
                    <Label htmlFor="email" className="text-sm font-medium text-slate-700">
                      Email Address
                    </Label>
                    <Input
                      id="email"
                      type="email"
                      value={form.email}
                      onChange={(e) => setForm({ ...form, email: e.target.value })}
                      placeholder="user@example.com"
                      required
                      className="h-12 border-slate-200 focus:border-blue-500 focus:ring-blue-500"
                    />
                  </div>

                  {/* Role Field */}
                  <div className="space-y-2">
                    <Label htmlFor="role" className="text-sm font-medium text-slate-700">
                      User Role
                    </Label>
                    <Select
                      value={form.role}
                      onValueChange={(value) => setForm({ ...form, role: value })}
                    >
                      <SelectTrigger className="h-12 border-slate-200 focus:border-blue-500 focus:ring-blue-500">
                        <SelectValue placeholder="Select user role" />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="user">
                          <div className="flex items-center gap-2">
                            <User className="w-4 h-4" />
                            User
                          </div>
                        </SelectItem>
                        <SelectItem value="admin">
                          <div className="flex items-center gap-2">
                            <Shield className="w-4 h-4" />
                            Administrator
                          </div>
                        </SelectItem>
                      </SelectContent>
                    </Select>
                    <p className="text-xs text-slate-500">
                      Administrators have full access to all features and settings
                    </p>
                  </div>

                  {/* Organization Field */}
                  <div className="space-y-2">
                    <Label htmlFor="organization" className="text-sm font-medium text-slate-700">
                      Organization
                    </Label>
                    <Select
                      value={form.organizationId || 'none'}
                      onValueChange={(value) => setForm({ ...form, organizationId: value === 'none' ? undefined : value })}
                    >
                      <SelectTrigger className="h-12 border-slate-200 focus:border-blue-500 focus:ring-blue-500">
                        <SelectValue placeholder="Select organization (optional)" />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="none">
                          <div className="flex items-center gap-2 text-slate-500">
                            <Building2 className="w-4 h-4" />
                            No organization
                          </div>
                        </SelectItem>
                        {organizations.map((org) => (
                          <SelectItem key={org.id} value={org.id}>
                            <div className="flex items-center gap-2">
                              <div className="w-6 h-6 rounded bg-gradient-to-br from-blue-500 to-purple-600 flex items-center justify-center text-white text-xs font-semibold">
                                {org.name.charAt(0).toUpperCase()}
                              </div>
                              <div>
                                <div className="font-medium">{org.name}</div>
                                <div className="text-xs text-slate-500">{org.domain}</div>
                              </div>
                              {getOrgBadge(org.status)}
                            </div>
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                    <p className="text-xs text-slate-500">
                      Users can be assigned to organizations for multi-tenant access control
                    </p>
                  </div>
                </CardContent>

                <CardFooter className="justify-between border-t bg-slate-50">
                  <Button
                    type="button"
                    variant="outline"
                    onClick={() => router.push('/admin/users')}
                    className="border-slate-200 hover:bg-slate-100"
                  >
                    Cancel
                  </Button>
                  <Button
                    type="submit"
                    disabled={submitting}
                    className="bg-gradient-to-r from-blue-600 to-purple-600 hover:from-blue-700 hover:to-purple-700"
                  >
                    {submitting ? (
                      <>
                        <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                        Saving Changes...
                      </>
                    ) : (
                      <>
                        <Save className="w-4 h-4 mr-2" />
                        Save Changes
                      </>
                    )}
                  </Button>
                </CardFooter>
              </form>
            )}
          </Card>

          {/* Help Section */}
          <Card className="mt-6 border-0 bg-gradient-to-br from-card to-card/50">
            <CardHeader>
              <CardTitle className="text-lg">Organization Assignment</CardTitle>
              <CardDescription>
                Understanding user organization relationships
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              <div className="grid md:grid-cols-2 gap-4 text-sm">
                <div className="space-y-2">
                  <h4 className="font-medium text-slate-900">No Organization</h4>
                  <p className="text-slate-600">
                    Users without organization assignment have access to global features but cannot access organization-specific content.
                  </p>
                </div>
                <div className="space-y-2">
                  <h4 className="font-medium text-slate-900">Organization Member</h4>
                  <p className="text-slate-600">
                    Users assigned to organizations can access organization-specific features and collaborate with team members.
                  </p>
                </div>
              </div>

              <Alert>
                <Building2 className="h-4 w-4" />
                <AlertDescription>
                  Organization assignment affects data access and feature availability. Make sure to assign users to the correct organization for their role.
                </AlertDescription>
              </Alert>
            </CardContent>
          </Card>
        </div>
      </div>
    </div>
  );
}
