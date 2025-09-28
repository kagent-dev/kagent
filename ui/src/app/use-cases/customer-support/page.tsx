'use client';

import Link from 'next/link';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';

export default function CustomerSupportUseCase() {
  return (
    <div className="min-h-screen">
      <section className="relative overflow-hidden">
        <div className="relative max-w-6xl mx-auto px-6 py-16 md:py-24">
          <div className="grid md:grid-cols-2 gap-10 items-center">
            <div>
              <h1 className="text-4xl md:text-5xl font-bold tracking-tight leading-tight">Customer Support</h1>
              <p className="mt-5 text-lg text-muted-foreground">
                Resolve tickets faster with AI agents that summarize chats, retrieve knowledge, and hand off with full context.
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
                  src="https://images.unsplash.com/photo-1525182008055-f88b95ff7980?q=80&w=1400&auto=format&fit=crop"
                  alt="Customer support agent"
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
              <CardTitle>Deflect and Escalate</CardTitle>
              <CardDescription>Self-serve first; handoff when needed.</CardDescription>
            </CardHeader>
            <CardContent>
              <ul className="text-sm text-muted-foreground list-disc pl-5 space-y-1">
                <li>Multi-channel: chat, email, web</li>
                <li>Ticket summarization</li>
                <li>Auto-tagging and routing</li>
              </ul>
            </CardContent>
          </Card>
          <Card>
            <CardHeader>
              <CardTitle>Grounded Answers</CardTitle>
              <CardDescription>RAG over docs, FAQs, and past resolutions.</CardDescription>
            </CardHeader>
            <CardContent>
              <ul className="text-sm text-muted-foreground list-disc pl-5 space-y-1">
                <li>Permissions-aware retrieval</li>
                <li>Freshness and versioning</li>
                <li>Feedback loops</li>
              </ul>
            </CardContent>
          </Card>
          <Card>
            <CardHeader>
              <CardTitle>Quality & Analytics</CardTitle>
              <CardDescription>Track outcomes and customer sentiment.</CardDescription>
            </CardHeader>
            <CardContent>
              <ul className="text-sm text-muted-foreground list-disc pl-5 space-y-1">
                <li>CSAT, FCR, time-to-resolution</li>
                <li>Guardrails and review</li>
                <li>Observability built-in</li>
              </ul>
            </CardContent>
          </Card>
        </div>
      </section>
    </div>
  );
}
