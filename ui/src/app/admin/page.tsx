'use client';

import { useEffect, useState } from 'react';
import { useRouter } from 'next/navigation';
import Link from 'next/link';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Users, Server, Activity, CreditCard, Building2 } from 'lucide-react';
import { useAuth } from '@/contexts/AuthContext';

export default function AdminHomePage() {
  const { user, isAuthenticated, isLoading } = useAuth();
  const router = useRouter();
  const [isCheckingAuth, setIsCheckingAuth] = useState(true);

  useEffect(() => {
    if (isLoading) return;
    
    if (!isAuthenticated) {
      router.push('/login');
      return;
    }
    
    if (user?.role !== 'admin') {
      router.push('/unauthorized');
      return;
    }
    
    setIsCheckingAuth(false);
  }, [isAuthenticated, user, router, isLoading]);
  
  if (isLoading || isCheckingAuth) {
    return (
      <div className="flex items-center justify-center min-h-screen">
        <div className="animate-spin rounded-full h-12 w-12 border-t-2 border-b-2 border-primary"></div>
      </div>
    );
  }

  return (
    <div className="container mx-auto px-4 py-8 space-y-8">
      <div>
        <h1 className="text-3xl font-bold">Admin</h1>
        <p className="text-muted-foreground">Manage users, gateways, and observability.</p>
      </div>

      <div className="grid md:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5 gap-6">
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2"><Users className="h-5 w-5" /> Users</CardTitle>
            <CardDescription>Manage user accounts and roles</CardDescription>
          </CardHeader>
          <CardContent className="flex justify-between items-center">
            <div className="text-sm text-muted-foreground">Create, edit, and remove users.</div>
            <Button asChild><Link href="/admin/users">Open</Link></Button>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2"><Building2 className="h-5 w-5" /> Organizations</CardTitle>
            <CardDescription>Multi-tenant organization management</CardDescription>
          </CardHeader>
          <CardContent className="flex justify-between items-center">
            <div className="text-sm text-muted-foreground">Manage organizations and their users.</div>
            <Button asChild variant="outline"><Link href="/admin/organizations">Open</Link></Button>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">Models Settings</CardTitle>
            <CardDescription>Enable/disable models</CardDescription>
          </CardHeader>
          <CardContent className="flex justify-between items-center">
            <div className="text-sm text-muted-foreground">Control available models for selection.</div>
            <Button asChild variant="outline"><Link href="/admin/models-settings">Open</Link></Button>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2"><Users className="h-5 w-5" /> Agents Settings</CardTitle>
            <CardDescription>Enable/disable agent visibility</CardDescription>
          </CardHeader>
          <CardContent className="flex justify-between items-center">
            <div className="text-sm text-muted-foreground">Control which agents appear on /agents.</div>
            <Button asChild variant="outline"><Link href="/admin/agents-settings">Open</Link></Button>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2"><Server className="h-5 w-5" /> Agent Gateway</CardTitle>
            <CardDescription>Public access and branding</CardDescription>
          </CardHeader>
          <CardContent className="flex justify-between items-center">
            <div className="text-sm text-muted-foreground">Enable, URL, theme, and auth.</div>
            <Button asChild variant="outline"><Link href="/admin/agentgateway">Open</Link></Button>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2"><Activity className="h-5 w-5" /> Observability</CardTitle>
            <CardDescription>Tracing and analytics</CardDescription>
          </CardHeader>
          <CardContent className="flex justify-between items-center">
            <div className="text-sm text-muted-foreground">Provider, endpoint, sampling.</div>
            <Button asChild><Link href="/admin/observability">Open</Link></Button>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2"><CreditCard className="h-5 w-5" /> Pricing & Usage</CardTitle>
            <CardDescription>Cluster cost and capacity</CardDescription>
          </CardHeader>
          <CardContent className="flex justify-between items-center">
            <div className="text-sm text-muted-foreground">Costs, nodes, CPU, memory.</div>
            <Button asChild variant="outline"><Link href="/admin/pricing">Open</Link></Button>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">SRE</CardTitle>
            <CardDescription>SLAs, SLOs, SLIs</CardDescription>
          </CardHeader>
          <CardContent className="flex justify-between items-center">
            <div className="text-sm text-muted-foreground">Availability, latency, errors.</div>
            <Button asChild><Link href="/admin/sre">Open</Link></Button>
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
