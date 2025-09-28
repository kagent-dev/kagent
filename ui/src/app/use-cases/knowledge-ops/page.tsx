'use client';

import Link from 'next/link';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';

export default function KnowledgeOpsUseCase() {
  return (
    <div className="min-h-screen">
      <section className="relative overflow-hidden">
        <div className="relative max-w-6xl mx-auto px-6 py-16 md:py-24">
          <div className="grid md:grid-cols-2 gap-10 items-center">
            <div>
              <h1 className="text-4xl md:text-5xl font-bold tracking-tight leading-tight">Knowledge Ops</h1>
              <p className="mt-5 text-lg text-muted-foreground">
                Ground responses in enterprise knowledge with RAG, permissions, and freshness built-in.
              </p>
              <div className="mt-8 flex gap-3">
                <Button asChild>
                  <Link href="/signup">Start Free</Link>
                </Button>
                <Button variant="outline" asChild>
                  <Link href="/company/contact">Talk to Sales</Link>
                </Button>
              </div>
            </div>
            <div className="relative">
              <div className="rounded-xl overflow-hidden ring-1 ring-border shadow-2xl bg-card">
                <img
                  src="https://images.unsplash.com/photo-1532012197267-da84d127e765?q=80&w=1400&auto=format&fit=crop"
                  alt="Knowledge operations"
                  className="w-full h-[320px] object-cover"
                />
              </div>
            </div>
          </div>
        </div>
      </section>

      <section className="py-16 md:py-24 border-t">
        <div className="max-w-6xl mx-auto px-6 grid md:grid-cols-3 gap-6">
          <Card>
            <CardHeader>
              <CardTitle>RAG with Governance</CardTitle>
              <CardDescription>Bring your own sources with access controls.</CardDescription>
            </CardHeader>
            <CardContent>
              <ul className="text-sm text-muted-foreground list-disc pl-5 space-y-1">
                <li>ACL and row-level security</li>
                <li>Embeddings and cache controls</li>
                <li>PII redaction</li>
              </ul>
            </CardContent>
          </Card>
          <Card>
            <CardHeader>
              <CardTitle>Freshness & Lineage</CardTitle>
              <CardDescription>Know exactly what was used and when.</CardDescription>
            </CardHeader>
            <CardContent>
              <ul className="text-sm text-muted-foreground list-disc pl-5 space-y-1">
                <li>Source timestamps</li>
                <li>Link back to originals</li>
                <li>Human review queues</li>
              </ul>
            </CardContent>
          </Card>
          <Card>
            <CardHeader>
              <CardTitle>Developer Friendly</CardTitle>
              <CardDescription>SDKs, APIs, and examples to move fast.</CardDescription>
            </CardHeader>
            <CardContent>
              <ul className="text-sm text-muted-foreground list-disc pl-5 space-y-1">
                <li>Tool APIs for ingestion</li>
                <li>Observability hooks</li>
                <li>CI-ready pipelines</li>
              </ul>
            </CardContent>
          </Card>
        </div>
      </section>
    </div>
  );
}
