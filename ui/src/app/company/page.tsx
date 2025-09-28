'use client';

import Link from 'next/link';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';

export default function CompanyPage() {
  return (
    <div className="max-w-6xl mx-auto px-6 py-12 space-y-10">
      <div className="text-center space-y-3">
        <h1 className="text-4xl md:text-5xl font-bold">Company</h1>
        <p className="text-muted-foreground max-w-2xl mx-auto">Learn about our mission and get in touch.</p>
      </div>

      <div className="grid md:grid-cols-3 gap-6">
        <Card>
          <CardHeader>
            <CardTitle>About</CardTitle>
            <CardDescription>Who we are and what we value.</CardDescription>
          </CardHeader>
          <CardContent className="flex items-center justify-between">
            <div className="text-sm text-muted-foreground">Our story and vision.</div>
            <Button asChild><Link href="/company/about">Read</Link></Button>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Contact</CardTitle>
            <CardDescription>We'd love to hear from you.</CardDescription>
          </CardHeader>
          <CardContent className="flex items-center justify-between">
            <div className="text-sm text-muted-foreground">Reach out to our team.</div>
            <Button asChild variant="outline"><Link href="/company/contact">Get in touch</Link></Button>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Status</CardTitle>
            <CardDescription>Platform uptime and incidents.</CardDescription>
          </CardHeader>
          <CardContent className="flex items-center justify-between">
            <div className="text-sm text-muted-foreground">See current status.</div>
            <Button asChild><Link href="/company/status">View</Link></Button>
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
