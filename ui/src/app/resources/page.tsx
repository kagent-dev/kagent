'use client';

import Link from 'next/link';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';

export default function ResourcesPage() {
  return (
    <div className="max-w-6xl mx-auto px-6 py-12 space-y-10">
      <div className="text-center space-y-3">
        <h1 className="text-4xl md:text-5xl font-bold">Resources</h1>
        <p className="text-muted-foreground max-w-2xl mx-auto">Documentation, guides, and examples to help you build faster.</p>
      </div>

      <div className="grid md:grid-cols-3 gap-6">
        <Card>
          <CardHeader>
            <CardTitle>Docs</CardTitle>
            <CardDescription>Concepts, APIs, and setup.</CardDescription>
          </CardHeader>
          <CardContent className="flex items-center justify-between">
            <p className="text-sm text-muted-foreground">Start with the fundamentals.</p>
            <Button asChild><Link href="/resources/docs">Read</Link></Button>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Guides</CardTitle>
            <CardDescription>Step-by-step walkthroughs.</CardDescription>
          </CardHeader>
          <CardContent className="flex items-center justify-between">
            <p className="text-sm text-muted-foreground">Build core flows quickly.</p>
            <Button asChild variant="outline"><Link href="/resources/guides">Read</Link></Button>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Examples</CardTitle>
            <CardDescription>Copy, run, and customize.</CardDescription>
          </CardHeader>
          <CardContent className="flex items-center justify-between">
            <p className="text-sm text-muted-foreground">Real-world starters.</p>
            <Button asChild><Link href="/resources/examples">Explore</Link></Button>
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
