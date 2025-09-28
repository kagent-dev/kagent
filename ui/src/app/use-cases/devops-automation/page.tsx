'use client';

import Link from 'next/link';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';

export default function DevOpsAutomationUseCase() {
  return (
    <div className="min-h-screen">
      <section className="relative overflow-hidden">
        <div className="relative max-w-6xl mx-auto px-6 py-16 md:py-24">
          <div className="grid md:grid-cols-2 gap-10 items-center">
            <div>
              <h1 className="text-4xl md:text-5xl font-bold tracking-tight leading-tight">DevOps Automation</h1>
              <p className="mt-5 text-lg text-muted-foreground">
                Triage incidents, run playbooks, and automate ops with safe, observable agent workflows.
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
                  src="https://images.unsplash.com/photo-1558494949-ef010cbdcc31?q=80&w=1400&auto=format&fit=crop"
                  alt="DevOps automation"
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
              <CardTitle>Incident Triage</CardTitle>
              <CardDescription>Summarize alerts and propose next steps.</CardDescription>
            </CardHeader>
            <CardContent>
              <ul className="text-sm text-muted-foreground list-disc pl-5 space-y-1">
                <li>Log and metric enrichment</li>
                <li>Knowledge-linked remediation</li>
                <li>Pager and ticket integration</li>
              </ul>
            </CardContent>
          </Card>
          <Card>
            <CardHeader>
              <CardTitle>Runbooks as Agents</CardTitle>
              <CardDescription>Execute playbooks safely with approvals.</CardDescription>
            </CardHeader>
            <CardContent>
              <ul className="text-sm text-muted-foreground list-disc pl-5 space-y-1">
                <li>Dry-run guardrails</li>
                <li>Change windows & RBAC</li>
                <li>Full auditability</li>
              </ul>
            </CardContent>
          </Card>
          <Card>
            <CardHeader>
              <CardTitle>ChatOps</CardTitle>
              <CardDescription>Workflows in Slack or Teams with context.</CardDescription>
            </CardHeader>
            <CardContent>
              <ul className="text-sm text-muted-foreground list-disc pl-5 space-y-1">
                <li>Role-aware actions</li>
                <li>Approval steps</li>
                <li>Persistent threads</li>
              </ul>
            </CardContent>
          </Card>
        </div>
      </section>
    </div>
  );
}
