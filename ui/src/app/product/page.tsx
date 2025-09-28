'use client';

import Link from 'next/link';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';

export default function ProductPage() {
  return (
    <div className="max-w-6xl mx-auto px-6 py-12 space-y-10">
      <div className="text-center space-y-3">
        <h1 className="text-4xl md:text-5xl font-bold">Product</h1>
        <p className="text-muted-foreground max-w-2xl mx-auto">
          Open‑source agentic AI platform for building, running, and scaling AI agents.
        </p>
      </div>

      <div className="grid md:grid-cols-2 gap-6">
        <Card>
          <CardHeader>
            <CardTitle>Agents</CardTitle>
            <CardDescription>Design and orchestrate intelligent agents.</CardDescription>
          </CardHeader>
          <CardContent className="flex items-center justify-between">
            <p className="text-sm text-muted-foreground">Compose models, tools, and instructions.</p>
            <Button asChild><Link href="/agents">Explore</Link></Button>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Models</CardTitle>
            <CardDescription>Manage model providers and configurations.</CardDescription>
          </CardHeader>
          <CardContent className="flex items-center justify-between">
            <p className="text-sm text-muted-foreground">Bring your own API keys and settings.</p>
            <Button asChild variant="outline"><Link href="/models">Explore</Link></Button>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Tools</CardTitle>
            <CardDescription>Extend agents with capabilities via tools.</CardDescription>
          </CardHeader>
          <CardContent className="flex items-center justify-between">
            <p className="text-sm text-muted-foreground">Create and integrate custom tools.</p>
            <Button asChild><Link href="/tools">Explore</Link></Button>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Tool Servers</CardTitle>
            <CardDescription>Secure, scalable tool hosting.</CardDescription>
          </CardHeader>
          <CardContent className="flex items-center justify-between">
            <p className="text-sm text-muted-foreground">Run tools close to your data and systems.</p>
            <Button asChild variant="outline"><Link href="/servers">Explore</Link></Button>
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
