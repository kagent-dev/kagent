'use client';

import { useEffect, useState } from 'react';
import { useRouter, usePathname } from 'next/navigation';
import Link from 'next/link';
import { FileText, BookOpen, Code, Settings, Zap, MessageSquare, Database, Cpu, Server, GitBranch } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '@/components/ui/card';
import { Skeleton } from '@/components/ui/skeleton';

const docs = [
  {
    id: 'overview',
    title: 'Overview',
    description: 'Introduction to Adolphe.AI and its core features',
    icon: <BookOpen className="h-5 w-5" />,
    category: 'Getting Started'
  },
  {
    id: 'quickstart',
    title: 'Quick Start',
    description: 'Get up and running in minutes',
    icon: <Zap className="h-5 w-5" />,
    category: 'Getting Started'
  },
  {
    id: 'agents',
    title: 'Agents',
    description: 'Create and manage AI agents',
    icon: <Cpu className="h-5 w-5" />,
    category: 'Core Concepts'
  },
  {
    id: 'api',
    title: 'API Reference',
    description: 'Complete API documentation',
    icon: <Code className="h-5 w-5" />,
    category: 'Core Concepts'
  },
  {
    id: 'development',
    title: 'Development',
    description: 'Set up your development environment',
    icon: <Code className="h-5 w-5" />,
    category: 'Development'
  },
  {
    id: 'architecture',
    title: 'Architecture',
    description: 'System design and components',
    icon: <Server className="h-5 w-5" />,
    category: 'Development'
  },
  {
    id: 'deployment',
    title: 'Deployment',
    description: 'Deploy to production',
    icon: <GitBranch className="h-5 w-5" />,
    category: 'Deployment'
  },
  {
    id: 'integrations',
    title: 'Integrations',
    description: 'Connect with external services',
    icon: <Database className="h-5 w-5" />,
    category: 'Development'
  }
];

const categories = [
  'Getting Started',
  'Core Concepts',
  'Development',
  'Deployment'
];

export default function DocsPage() {
  const router = useRouter();
  const pathname = usePathname();
  const [activeDoc, setActiveDoc] = useState('overview');
  const [content, setContent] = useState('');
  const [isLoading, setIsLoading] = useState(true);

  useEffect(() => {
    const loadContent = async () => {
      try {
        setIsLoading(true);
        const response = await fetch(`/resources/docs/${activeDoc}.md`);
        if (response.ok) {
          const text = await response.text();
          setContent(text);
        } else {
          setContent('# Documentation not found\nThe requested documentation could not be loaded.');
        }
      } catch (error) {
        console.error('Error loading documentation:', error);
        setContent('# Error loading documentation\nPlease try again later.');
      } finally {
        setIsLoading(false);
      }
    };

    loadContent();
  }, [activeDoc]);

  return (
    <div className="flex min-h-screen bg-background">
      {/* Sidebar */}
      <div className="w-64 border-r bg-muted/40 p-4 overflow-y-auto flex-shrink-0">
        <div className="mb-6">
          <h2 className="text-lg font-semibold mb-2">Documentation</h2>
          <p className="text-sm text-muted-foreground">
            Explore guides and resources to use Adolphe.AI
          </p>
        </div>

        <div className="space-y-6">
          {categories.map((category) => (
            <div key={category} className="space-y-2">
              <h3 className="text-sm font-medium text-muted-foreground uppercase tracking-wider">
                {category}
              </h3>
              <div className="space-y-1">
                {docs
                  .filter((doc) => doc.category === category)
                  .map((doc) => (
                    <Button
                      key={doc.id}
                      variant={activeDoc === doc.id ? 'secondary' : 'ghost'}
                      className={`w-full justify-start ${activeDoc === doc.id ? 'bg-accent' : ''}`}
                      onClick={() => setActiveDoc(doc.id)}
                    >
                      {doc.icon}
                      <span className="ml-2">{doc.title}</span>
                    </Button>
                  ))}
              </div>
            </div>
          ))}
        </div>
      </div>

      {/* Main Content */}
      <div className="flex-1 overflow-y-auto p-8">
        <div className="mx-auto max-w-4xl w-full">
          {isLoading ? (
            <div className="space-y-4">
              <Skeleton className="h-10 w-3/4" />
              <Skeleton className="h-6 w-1/2" />
              <div className="space-y-2 pt-4">
                <Skeleton className="h-4 w-full" />
                <Skeleton className="h-4 w-5/6" />
                <Skeleton className="h-4 w-4/5" />
              </div>
            </div>
          ) : (
            <div className="prose dark:prose-invert w-full">
              <h1 className="text-3xl font-bold tracking-tight mb-6">
                {docs.find(doc => doc.id === activeDoc)?.title}
              </h1>
              <div 
                dangerouslySetInnerHTML={{ __html: content }} 
                className="prose dark:prose-invert prose-headings:font-semibold prose-h2:text-2xl prose-h3:text-xl prose-p:text-foreground/80 max-w-none"
              />
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
