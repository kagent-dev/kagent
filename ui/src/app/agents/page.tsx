'use client';

import { useState, useEffect, useMemo } from 'react';
import { useRouter } from 'next/navigation';
import Link from 'next/link';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { AgentGrid } from '@/components/AgentGrid';
import { ErrorState } from '@/components/ErrorState';
import { LoadingState } from '@/components/LoadingState';
import { useAgents } from '@/components/AgentsProvider';
import KagentLogo from '@/components/kagent-logo';
import {
  Bot,
  Brain,
  Zap,
  Rocket,
  Sparkles,
  Plus,
  Search,
  Filter,
  Grid,
  List,
  Star,
  Clock,
  Users,
  Activity,
  TrendingUp,
  ArrowRight,
  CheckCircle,
  Pencil
} from 'lucide-react';

export default function AgentsPage() {
  const { agents, loading, error } = useAgents();
  const router = useRouter();
  const [enabledMap, setEnabledMap] = useState<Record<string, boolean>>({});
  const [viewMode, setViewMode] = useState<'grid' | 'list'>('grid');
  const [searchQuery, setSearchQuery] = useState('');
  const [selectedCategory, setSelectedCategory] = useState<string>('all');

  useEffect(() => {
    const load = async () => {
      try {
        const res = await fetch('/api/admin/agents-settings');
        if (res.ok) {
          const data = await res.json();
          setEnabledMap(data.enabled || {});
        }
      } catch {
        // ignore and default to all enabled
      }
    };
    load();
  }, []);

  const visibleAgents = useMemo(() => {
    return (agents || []).filter(a => {
      const ns = a.agent.metadata.namespace || '';
      const name = a.agent.metadata.name;
      const ref = `${ns}/${name}`;
      const flag = enabledMap.hasOwnProperty(ref) ? enabledMap[ref] : true;

      // Apply search filter
      const matchesSearch = searchQuery === '' ||
        name.toLowerCase().includes(searchQuery.toLowerCase()) ||
        a.agent.spec.description?.toLowerCase().includes(searchQuery.toLowerCase());

      // Apply category filter
      const matchesCategory = selectedCategory === 'all' || selectedCategory === 'ready';

      return flag && matchesSearch && matchesCategory;
    });
  }, [agents, enabledMap, searchQuery, selectedCategory]);

  const categories = [
    { id: 'all', name: 'All Agents', count: agents?.length || 0, icon: Grid },
    { id: 'ready', name: 'Ready', count: visibleAgents.filter(a => a.deploymentReady && a.accepted).length, icon: CheckCircle },
  ];

  if (error) {
    return <ErrorState message={error} />;
  }

  if (loading) {
    return <LoadingState />;
  }

  return (
    <div className="min-h-screen bg-gradient-to-br from-slate-50 via-blue-50 to-indigo-50">
      {/* Hero Section */}
      <div className="relative overflow-hidden bg-gradient-to-br from-blue-600 via-purple-600 to-indigo-700">
        <div className="absolute inset-0 bg-black/20"></div>
        <div className="relative container mx-auto px-4 py-16 md:py-24">
          <div className="text-center text-white space-y-6">
            <div className="inline-flex items-center gap-2 px-4 py-2 rounded-full bg-white/10 backdrop-blur-sm border border-white/20 text-white text-sm font-medium">
              <Bot className="w-4 h-4" />
              AI Agent Gallery
            </div>

            <div className="space-y-4">
              <h1 className="text-4xl md:text-6xl font-bold bg-gradient-to-r from-white to-blue-100 bg-clip-text text-transparent">
                Intelligent AI Agents
              </h1>
              <p className="text-xl md:text-2xl text-blue-100 max-w-3xl mx-auto leading-relaxed">
                Deploy autonomous AI agents that learn, adapt, and execute complex workflows with enterprise-grade reliability
              </p>
            </div>

            <div className="flex flex-col sm:flex-row gap-4 justify-center items-center">
              <Button
                size="lg"
                className="bg-white text-blue-600 hover:bg-blue-50 font-semibold px-8 py-3 rounded-full shadow-lg hover:shadow-xl transition-all"
                onClick={() => router.push('/agents/new')}
              >
                <Plus className="w-5 h-5 mr-2" />
                Deploy New Agent
              </Button>
              <Button
                size="lg"
                variant="outline"
                className="border-white/30 text-white hover:bg-white/10 font-semibold px-8 py-3 rounded-full backdrop-blur-sm"
                onClick={() => router.push('/models')}
              >
                <Brain className="w-5 h-5 mr-2" />
                Explore Models
              </Button>
            </div>
          </div>
        </div>

        {/* Animated background elements */}
        <div className="absolute top-10 left-10 w-20 h-20 bg-white/10 rounded-full animate-pulse"></div>
        <div className="absolute top-32 right-20 w-16 h-16 bg-purple-300/20 rounded-full animate-bounce delay-1000"></div>
        <div className="absolute bottom-20 left-1/4 w-12 h-12 bg-blue-300/20 rounded-full animate-pulse delay-500"></div>
      </div>

      {/* Main Content */}
      <div className="container mx-auto px-4 py-12">
        <div className="max-w-7xl mx-auto">

          {/* Search and Filter Bar */}
          <div className="mb-8">
            <Card className="border-0 bg-white/80 backdrop-blur-sm shadow-lg">
              <CardContent className="p-6">
                <div className="flex flex-col md:flex-row gap-4 items-center">
                  {/* Search */}
                  <div className="relative flex-1">
                    <Search className="absolute left-3 top-1/2 transform -translate-y-1/2 text-slate-400 w-4 h-4" />
                    <input
                      type="text"
                      placeholder="Search agents by name or description..."
                      value={searchQuery}
                      onChange={(e) => setSearchQuery(e.target.value)}
                      className="w-full pl-10 pr-4 py-3 rounded-lg border border-slate-200 focus:border-blue-500 focus:ring-2 focus:ring-blue-500/20 outline-none"
                    />
                  </div>

                  {/* Category Filter */}
                  <div className="flex gap-2">
                    {categories.map((category) => {
                      const Icon = category.icon;
                      return (
                        <Button
                          key={category.id}
                          variant={selectedCategory === category.id ? "default" : "outline"}
                          onClick={() => setSelectedCategory(category.id)}
                          className={`flex items-center gap-2 ${
                            selectedCategory === category.id
                              ? 'bg-blue-600 hover:bg-blue-700'
                              : 'border-slate-200 hover:bg-slate-50'
                          }`}
                        >
                          <Icon className="w-4 h-4" />
                          {category.name}
                          <Badge variant="secondary" className="ml-1">
                            {category.count}
                          </Badge>
                        </Button>
                      );
                    })}
                  </div>

                  {/* View Toggle */}
                  <div className="flex border rounded-lg p-1 bg-white">
                    <Button
                      variant={viewMode === 'grid' ? 'default' : 'ghost'}
                      size="sm"
                      onClick={() => setViewMode('grid')}
                      className="px-3"
                    >
                      <Grid className="w-4 h-4" />
                    </Button>
                    <Button
                      variant={viewMode === 'list' ? 'default' : 'ghost'}
                      size="sm"
                      onClick={() => setViewMode('list')}
                      className="px-3"
                    >
                      <List className="w-4 h-4" />
                    </Button>
                  </div>
                </div>
              </CardContent>
            </Card>
          </div>

          {/* Stats Cards */}
          <div className="grid grid-cols-1 md:grid-cols-4 gap-6 mb-8">
            <Card className="group hover:shadow-xl transition-all duration-300 border-0 bg-gradient-to-br from-blue-500 to-blue-600 text-white hover:scale-105">
              <CardHeader className="pb-3">
                <div className="flex items-center justify-between">
                  <div className="w-12 h-12 rounded-xl bg-white/20 flex items-center justify-center backdrop-blur-sm">
                    <Bot className="w-6 h-6 text-white" />
                  </div>
                  <Badge className="bg-white/20 text-white border-white/30">
                    Total
                  </Badge>
                </div>
                <div>
                  <CardTitle className="text-2xl font-bold text-white">{agents?.length || 0}</CardTitle>
                  <CardDescription className="text-blue-100">AI Agents Available</CardDescription>
                </div>
              </CardHeader>
              <CardContent>
                <p className="text-blue-100 text-sm">
                  Collection of intelligent automation agents
                </p>
              </CardContent>
            </Card>

            <Card className="group hover:shadow-xl transition-all duration-300 border-0 bg-gradient-to-br from-green-500 to-green-600 text-white hover:scale-105">
              <CardHeader className="pb-3">
                <div className="flex items-center justify-between">
                  <div className="w-12 h-12 rounded-xl bg-white/20 flex items-center justify-center backdrop-blur-sm">
                    <Activity className="w-6 h-6 text-white" />
                  </div>
                  <Badge className="bg-white/20 text-white border-white/30">
                    Ready
                  </Badge>
                </div>
                <div>
                  <CardTitle className="text-2xl font-bold text-white">
                    {visibleAgents.filter(a => a.deploymentReady && a.accepted).length}
                  </CardTitle>
                  <CardDescription className="text-green-100">Deployed & Active</CardDescription>
                </div>
              </CardHeader>
              <CardContent>
                <p className="text-green-100 text-sm">
                  Agents ready for production use
                </p>
              </CardContent>
            </Card>

            <Card className="group hover:shadow-xl transition-all duration-300 border-0 bg-gradient-to-br from-purple-500 to-purple-600 text-white hover:scale-105">
              <CardHeader className="pb-3">
                <div className="flex items-center justify-between">
                  <div className="w-12 h-12 rounded-xl bg-white/20 flex items-center justify-center backdrop-blur-sm">
                    <Brain className="w-6 h-6 text-white" />
                  </div>
                  <Badge className="bg-white/20 text-white border-white/30">
                    Models
                  </Badge>
                </div>
                <div>
                  <CardTitle className="text-2xl font-bold text-white">
                    {new Set(agents?.map(a => a.modelProvider).filter(Boolean)).size || 0}
                  </CardTitle>
                  <CardDescription className="text-purple-100">AI Providers</CardDescription>
                </div>
              </CardHeader>
              <CardContent>
                <p className="text-purple-100 text-sm">
                  Multiple AI model providers supported
                </p>
              </CardContent>
            </Card>

            <Card className="group hover:shadow-xl transition-all duration-300 border-0 bg-gradient-to-br from-orange-500 to-orange-600 text-white hover:scale-105">
              <CardHeader className="pb-3">
                <div className="flex items-center justify-between">
                  <div className="w-12 h-12 rounded-xl bg-white/20 flex items-center justify-center backdrop-blur-sm">
                    <TrendingUp className="w-6 h-6 text-white" />
                  </div>
                  <Badge className="bg-white/20 text-white border-white/30">
                    Growth
                  </Badge>
                </div>
                <div>
                  <CardTitle className="text-2xl font-bold text-white">∞</CardTitle>
                  <CardDescription className="text-orange-100">Possibilities</CardDescription>
                </div>
              </CardHeader>
              <CardContent>
                <p className="text-orange-100 text-sm">
                  Limitless automation potential
                </p>
              </CardContent>
            </Card>
          </div>

          {/* Agents Section */}
          <div className="space-y-6">
            <div className="flex items-center justify-between">
              <div>
                <h2 className="text-3xl font-bold bg-gradient-to-r from-slate-900 to-slate-700 bg-clip-text text-transparent">
                  AI Agent Collection
                </h2>
                <p className="text-slate-600 mt-2">
                  {visibleAgents.length} agent{visibleAgents.length !== 1 ? 's' : ''} available
                  {searchQuery && ` matching "${searchQuery}"`}
                </p>
              </div>

              <Button
                className="bg-gradient-to-r from-blue-600 to-purple-600 hover:from-blue-700 hover:to-purple-700"
                onClick={() => router.push('/agents/new')}
              >
                <Plus className="w-4 h-4 mr-2" />
                Deploy Agent
              </Button>
            </div>

            {/* Agents Grid/List */}
            {visibleAgents.length === 0 ? (
              <Card className="border-0 bg-white/80 backdrop-blur-sm shadow-lg">
                <CardContent className="flex flex-col items-center justify-center py-16">
                  <div className="w-24 h-24 rounded-2xl bg-gradient-to-br from-blue-500 to-purple-600 flex items-center justify-center mb-6">
                    <Bot className="w-12 h-12 text-white" />
                  </div>
                  <h3 className="text-2xl font-bold text-slate-900 mb-2">No Agents Found</h3>
                  <p className="text-slate-600 text-center mb-6 max-w-md">
                    {searchQuery
                      ? `No agents match your search for "${searchQuery}". Try adjusting your search terms.`
                      : "Get started by deploying your first AI agent to automate your workflows."
                    }
                  </p>
                  <Button
                    className="bg-gradient-to-r from-blue-600 to-purple-600 hover:from-blue-700 hover:to-purple-700"
                    onClick={() => router.push('/agents/new')}
                  >
                    <Plus className="w-4 h-4 mr-2" />
                    Deploy Your First Agent
                  </Button>
                </CardContent>
              </Card>
            ) : (
              <div className={viewMode === 'grid'
                ? "grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6"
                : "space-y-4"
              }>
                {visibleAgents.map((agent) => (
                  <AgentCard key={agent.agent.metadata.name} agentResponse={agent} viewMode={viewMode} />
                ))}
              </div>
            )}
          </div>

          {/* Featured Agents Section */}
          {visibleAgents.length > 0 && (
            <div className="mt-12">
              <Card className="border-0 bg-gradient-to-br from-white to-slate-50 shadow-lg">
                <CardHeader>
                  <CardTitle className="flex items-center gap-2">
                    <Star className="w-6 h-6 text-yellow-500" />
                    Featured AI Agents
                  </CardTitle>
                  <CardDescription>
                    Popular and highly-rated agents for common automation tasks
                  </CardDescription>
                </CardHeader>
                <CardContent>
                  <div className="grid md:grid-cols-2 lg:grid-cols-3 gap-6">
                    {visibleAgents.slice(0, 3).map((agent) => (
                      <div key={agent.agent.metadata.name} className="p-4 rounded-lg border bg-white/50 hover:bg-white/80 transition-colors cursor-pointer">
                        <div className="flex items-center gap-3 mb-3">
                          <div className="w-10 h-10 rounded-xl bg-gradient-to-br from-blue-500 to-purple-600 flex items-center justify-center">
                            <Bot className="w-5 h-5 text-white" />
                          </div>
                          <div>
                            <h4 className="font-semibold text-slate-900">{agent.agent.metadata.name}</h4>
                            <p className="text-sm text-slate-600">{agent.modelProvider}</p>
                          </div>
                        </div>
                        <p className="text-sm text-slate-600 mb-3 line-clamp-2">
                          {agent.agent.spec.description}
                        </p>
                        <div className="flex items-center justify-between">
                          <div className="flex gap-2">
                            {agent.deploymentReady && (
                              <Badge className="bg-green-100 text-green-800">Ready</Badge>
                            )}
                            {agent.accepted && (
                              <Badge className="bg-blue-100 text-blue-800">Active</Badge>
                            )}
                          </div>
                          <Button size="sm" variant="outline" asChild>
                            <Link href={`/agents/${agent.agent.metadata.namespace}/${agent.agent.metadata.name}/chat`}>
                              Chat
                              <ArrowRight className="w-3 h-3 ml-1" />
                            </Link>
                          </Button>
                        </div>
                      </div>
                    ))}
                  </div>
                </CardContent>
              </Card>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

// Enhanced Agent Card Component
function AgentCard({ agentResponse, viewMode }: { agentResponse: any, viewMode: 'grid' | 'list' }) {
  const { agent, model, modelProvider, deploymentReady, accepted } = agentResponse;
  const router = useRouter();

  const agentRef = `${agent.metadata.namespace || ''}/${agent.metadata.name}`;
  const isBYO = agent.spec?.type === "BYO";
  const byoImage = isBYO ? agent.spec?.byo?.deployment?.image : undefined;

  const handleEditClick = (e: React.MouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
    router.push(`/agents/new?edit=true&name=${agent.metadata.name}&namespace=${agent.metadata.namespace}`);
  };

  if (viewMode === 'list') {
    return (
      <Card className="group hover:shadow-md transition-all duration-300 border-0 bg-white/80 backdrop-blur-sm">
        <CardContent className="p-6">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-4">
              <div className="w-12 h-12 rounded-xl bg-gradient-to-br from-blue-500 to-purple-600 flex items-center justify-center">
                <Bot className="w-6 h-6 text-white" />
              </div>
              <div>
                <h3 className="font-semibold text-slate-900">{agent.metadata.name}</h3>
                <p className="text-sm text-slate-600">{agentRef}</p>
              </div>
            </div>

            <div className="flex items-center gap-4">
              <div className="text-right">
                <p className="text-sm text-slate-600">{modelProvider}</p>
                <p className="text-xs text-slate-500">{model}</p>
              </div>

              <div className="flex gap-2">
                {deploymentReady && accepted && (
                  <Badge className="bg-green-100 text-green-800">Ready</Badge>
                )}
                {!accepted && (
                  <Badge className="bg-red-100 text-red-800">Pending</Badge>
                )}
                {!deploymentReady && (
                  <Badge className="bg-yellow-100 text-yellow-800">Building</Badge>
                )}
              </div>

              <div className="flex gap-2">
                <Button variant="outline" size="sm" onClick={handleEditClick}>
                  Edit
                </Button>
                {deploymentReady && accepted && (
                  <Button size="sm" asChild>
                    <Link href={`/agents/${agent.metadata.namespace}/${agent.metadata.name}/chat`}>
                      Chat
                    </Link>
                  </Button>
                )}
              </div>
            </div>
          </div>

          <p className="text-sm text-slate-600 mt-4 line-clamp-2">
            {agent.spec.description}
          </p>
        </CardContent>
      </Card>
    );
  }

  // Grid view (default)
  return (
    <Link href={deploymentReady && accepted ? `/agents/${agent.metadata.namespace}/${agent.metadata.name}/chat` : '#'} passHref>
      <Card className={`group transition-all duration-300 border-0 bg-white/80 backdrop-blur-sm hover:shadow-lg hover:scale-105 ${
        deploymentReady && accepted
          ? 'cursor-pointer hover:border-blue-300'
          : 'border-slate-200'
      }`}>
        <CardHeader className="pb-3">
          <div className="flex items-start justify-between">
            <div className="flex items-center gap-3">
              <div className="w-10 h-10 rounded-xl bg-gradient-to-br from-blue-500 to-purple-600 flex items-center justify-center">
                <Bot className="w-5 h-5 text-white" />
              </div>
              <div>
                <CardTitle className="text-lg">{agent.metadata.name}</CardTitle>
                <CardDescription className="text-sm">{agentRef}</CardDescription>
              </div>
            </div>

            <div className="flex gap-1 opacity-0 group-hover:opacity-100 transition-opacity">
              <Button
                variant="ghost"
                size="icon"
                onClick={handleEditClick}
                className="h-8 w-8"
              >
                <Pencil className="h-4 w-4" />
              </Button>
            </div>
          </div>

          <div className="flex gap-2 mt-2">
            {deploymentReady && accepted && (
              <Badge className="bg-green-100 text-green-800">Ready</Badge>
            )}
            {!accepted && (
              <Badge className="bg-red-100 text-red-800">Pending</Badge>
            )}
            {!deploymentReady && (
              <Badge className="bg-yellow-100 text-yellow-800">Building</Badge>
            )}
          </div>
        </CardHeader>

        <CardContent>
          <p className="text-sm text-slate-600 line-clamp-3 mb-4">
            {agent.spec.description}
          </p>

          <div className="flex items-center justify-between text-xs text-slate-500">
            <span>{modelProvider} • {model}</span>
            <div className="flex items-center gap-1">
              <Clock className="w-3 h-3" />
              Active
            </div>
          </div>
        </CardContent>
      </Card>
    </Link>
  );
}
