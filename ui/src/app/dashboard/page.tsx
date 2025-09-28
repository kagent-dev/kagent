'use client';

import { useState, useEffect } from 'react';
import { useRouter } from 'next/navigation';
import { useAuth } from '@/hooks/useAuth';
import { useAgents } from '@/components/AgentsProvider';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import Link from 'next/link';
import {
  Brain,
  Zap,
  Users,
  Bot,
  TrendingUp,
  Activity,
  Sparkles,
  Rocket,
  Shield,
  Cpu,
  Network,
  BarChart3,
  Settings,
  ArrowRight,
  Star,
  ChevronDown,
  ChevronUp
} from 'lucide-react';

export default function DashboardPage() {
  const { user, logout, isAuthenticated, isLoading } = useAuth();
  const { agents, models, loading: agentsLoading } = useAgents();
  const router = useRouter();
  const [isAccountExpanded, setIsAccountExpanded] = useState(false);
  const [apiUsage, setApiUsage] = useState({ totalRequests: 0, todayRequests: 0, lastUpdated: '' });

  // Load API usage statistics
  useEffect(() => {
    const loadApiUsage = () => {
      try {
        const stored = localStorage.getItem('api-usage-stats');
        if (stored) {
          const usage = JSON.parse(stored);
          setApiUsage(usage);
        } else {
          const initialUsage = { totalRequests: 0, todayRequests: 0, lastUpdated: '' };
          localStorage.setItem('api-usage-stats', JSON.stringify(initialUsage));
          setApiUsage(initialUsage);
        }
      } catch {
        const initialUsage = { totalRequests: 0, todayRequests: 0, lastUpdated: '' };
        localStorage.setItem('api-usage-stats', JSON.stringify(initialUsage));
        setApiUsage(initialUsage);
      }
    };
    loadApiUsage();
  }, []);

  // Track API usage
  const trackApiUsage = () => {
    const today = new Date().toDateString();
    const stored = localStorage.getItem('api-usage-stats');
    let usage = stored ? JSON.parse(stored) : { totalRequests: 0, todayRequests: 0, lastUpdated: '' };

    if (usage.lastUpdated !== today) {
      usage.todayRequests = 0;
      usage.lastUpdated = today;
    }

    usage.totalRequests += 1;
    usage.todayRequests += 1;

    localStorage.setItem('api-usage-stats', JSON.stringify(usage));
    setApiUsage(usage);
  };

  useEffect(() => {
    trackApiUsage();
    const interval = setInterval(trackApiUsage, 5 * 60 * 1000);
    return () => clearInterval(interval);
  }, []);

  // Redirect to login if not authenticated
  useEffect(() => {
    if (!isLoading && !isAuthenticated) {
      router.push('/login');
    }
  }, [isAuthenticated, isLoading, router]);

  if (isLoading) {
    return (
      <div className="min-h-screen bg-gradient-to-br from-slate-50 via-blue-50 to-indigo-50 flex items-center justify-center">
        <div className="text-center">
          <div className="animate-spin rounded-full h-16 w-16 border-t-4 border-b-4 border-blue-600 mx-auto mb-4"></div>
          <p className="text-slate-600 text-lg">Loading your AI dashboard...</p>
        </div>
      </div>
    );
  }

  if (!isAuthenticated) {
    return null;
  }

  const handleLogout = () => {
    logout();
    router.push('/');
  };

  // Calculate statistics
  const totalAgents = agents?.length || 0;
  const totalModels = models?.length || 0;
  const activeModels = totalModels;

  return (
    <div className="min-h-screen bg-gradient-to-br from-slate-50 via-blue-50 to-indigo-50">
      {/* Hero Section */}
      <div className="relative overflow-hidden bg-gradient-to-br from-blue-600 via-purple-600 to-indigo-700">
        <div className="absolute inset-0 bg-black/20"></div>
        <div className="relative container mx-auto px-4 py-16 md:py-24">
          <div className="text-center text-white space-y-6">
            <div className="inline-flex items-center gap-2 px-4 py-2 rounded-full bg-white/10 backdrop-blur-sm border border-white/20 text-white text-sm font-medium">
              <Brain className="w-4 h-4" />
              Welcome to Your AI Command Center
            </div>

            <div className="space-y-4">
              <h1 className="text-4xl md:text-6xl font-bold bg-gradient-to-r from-white to-blue-100 bg-clip-text text-transparent">
                Intelligent Automation
              </h1>
              <p className="text-xl md:text-2xl text-blue-100 max-w-3xl mx-auto leading-relaxed">
                Orchestrate powerful AI agents, deploy cutting-edge models, and build the future of enterprise automation
              </p>
            </div>

            <div className="flex flex-col sm:flex-row gap-4 justify-center items-center">
              <Button
                size="lg"
                className="bg-white text-blue-600 hover:bg-blue-50 font-semibold px-8 py-3 rounded-full shadow-lg hover:shadow-xl transition-all"
                asChild
              >
                <Link href="/agents">
                  <Bot className="w-5 h-5 mr-2" />
                  Explore AI Agents
                </Link>
              </Button>
              <Button
                size="lg"
                variant="outline"
                className="border-white/30 text-white hover:bg-white/10 font-semibold px-8 py-3 rounded-full backdrop-blur-sm"
                asChild
              >
                <Link href="/models">
                  <Cpu className="w-5 h-5 mr-2" />
                  Configure Models
                </Link>
              </Button>
            </div>
          </div>
        </div>

        {/* Animated background elements */}
        <div className="absolute top-10 left-10 w-20 h-20 bg-white/10 rounded-full animate-pulse"></div>
        <div className="absolute top-32 right-20 w-16 h-16 bg-purple-300/20 rounded-full animate-bounce delay-1000"></div>
        <div className="absolute bottom-20 left-1/4 w-12 h-12 bg-blue-300/20 rounded-full animate-pulse delay-500"></div>
      </div>

      {/* Main Dashboard Content */}
      <div className="container mx-auto px-4 py-12">
        <div className="max-w-7xl mx-auto space-y-12">

          {/* AI Statistics Overview */}
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-6">
            <Card className="group hover:shadow-xl transition-all duration-300 border-0 bg-gradient-to-br from-blue-500 to-blue-600 text-white hover:scale-105">
              <CardHeader className="pb-3">
                <div className="flex items-center justify-between">
                  <div className="w-12 h-12 rounded-xl bg-white/20 flex items-center justify-center backdrop-blur-sm">
                    <Bot className="w-6 h-6 text-white" />
                  </div>
                  <Badge className="bg-white/20 text-white border-white/30">
                    Active
                  </Badge>
                </div>
                <div>
                  <CardTitle className="text-2xl font-bold text-white">{totalAgents}</CardTitle>
                  <CardDescription className="text-blue-100">AI Agents Deployed</CardDescription>
                </div>
              </CardHeader>
              <CardContent>
                <p className="text-blue-100 text-sm">
                  Intelligent agents automating your workflows
                </p>
              </CardContent>
            </Card>

            <Card className="group hover:shadow-xl transition-all duration-300 border-0 bg-gradient-to-br from-purple-500 to-purple-600 text-white hover:scale-105">
              <CardHeader className="pb-3">
                <div className="flex items-center justify-between">
                  <div className="w-12 h-12 rounded-xl bg-white/20 flex items-center justify-center backdrop-blur-sm">
                    <Cpu className="w-6 h-6 text-white" />
                  </div>
                  <Badge className="bg-white/20 text-white border-white/30">
                    Ready
                  </Badge>
                </div>
                <div>
                  <CardTitle className="text-2xl font-bold text-white">{activeModels}</CardTitle>
                  <CardDescription className="text-purple-100">AI Models Available</CardDescription>
                </div>
              </CardHeader>
              <CardContent>
                <p className="text-purple-100 text-sm">
                  Advanced language and vision models
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
                    Today
                  </Badge>
                </div>
                <div>
                  <CardTitle className="text-2xl font-bold text-white">{apiUsage.todayRequests.toLocaleString()}</CardTitle>
                  <CardDescription className="text-green-100">API Requests</CardDescription>
                </div>
              </CardHeader>
              <CardContent>
                <p className="text-green-100 text-sm">
                  Real-time usage tracking
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
                  <CardDescription className="text-orange-100">Potential</CardDescription>
                </div>
              </CardHeader>
              <CardContent>
                <p className="text-orange-100 text-sm">
                  Limitless automation possibilities
                </p>
              </CardContent>
            </Card>
          </div>

          {/* AI Features Showcase */}
          <div className="space-y-8">
            <div className="text-center space-y-4">
              <h2 className="text-3xl md:text-4xl font-bold bg-gradient-to-r from-slate-900 to-slate-700 bg-clip-text text-transparent">
                AI-Powered Capabilities
              </h2>
              <p className="text-lg text-slate-600 max-w-2xl mx-auto">
                Experience the future of enterprise automation with our advanced AI agent platform
              </p>
            </div>

            <div className="grid md:grid-cols-2 lg:grid-cols-3 gap-8">
              <Card className="group hover:shadow-lg transition-all duration-300 border-0 bg-white/80 backdrop-blur-sm hover:scale-105">
                <CardHeader>
                  <div className="w-14 h-14 rounded-2xl bg-gradient-to-br from-blue-500 to-purple-600 flex items-center justify-center mb-4 group-hover:scale-110 transition-transform">
                    <Brain className="w-7 h-7 text-white" />
                  </div>
                  <CardTitle className="text-xl">Intelligent Agents</CardTitle>
                  <CardDescription>
                    Deploy autonomous AI agents that learn, adapt, and execute complex workflows
                  </CardDescription>
                </CardHeader>
                <CardContent>
                  <Button variant="outline" className="w-full group-hover:bg-blue-50" asChild>
                    <Link href="/agents">
                      Explore Agents
                      <ArrowRight className="w-4 h-4 ml-2" />
                    </Link>
                  </Button>
                </CardContent>
              </Card>

              <Card className="group hover:shadow-lg transition-all duration-300 border-0 bg-white/80 backdrop-blur-sm hover:scale-105">
                <CardHeader>
                  <div className="w-14 h-14 rounded-2xl bg-gradient-to-br from-purple-500 to-pink-600 flex items-center justify-center mb-4 group-hover:scale-110 transition-transform">
                    <Network className="w-7 h-7 text-white" />
                  </div>
                  <CardTitle className="text-xl">Model Orchestration</CardTitle>
                  <CardDescription>
                    Seamlessly integrate multiple AI models for comprehensive automation solutions
                  </CardDescription>
                </CardHeader>
                <CardContent>
                  <Button variant="outline" className="w-full group-hover:bg-purple-50" asChild>
                    <Link href="/models">
                      Configure Models
                      <ArrowRight className="w-4 h-4 ml-2" />
                    </Link>
                  </Button>
                </CardContent>
              </Card>

              <Card className="group hover:shadow-lg transition-all duration-300 border-0 bg-white/80 backdrop-blur-sm hover:scale-105">
                <CardHeader>
                  <div className="w-14 h-14 rounded-2xl bg-gradient-to-br from-green-500 to-teal-600 flex items-center justify-center mb-4 group-hover:scale-110 transition-transform">
                    <Shield className="w-7 h-7 text-white" />
                  </div>
                  <CardTitle className="text-xl">Enterprise Security</CardTitle>
                  <CardDescription>
                    Military-grade security with end-to-end encryption and compliance features
                  </CardDescription>
                </CardHeader>
                <CardContent>
                  <Button variant="outline" className="w-full group-hover:bg-green-50" asChild>
                    <Link href="/admin">
                      Security Settings
                      <ArrowRight className="w-4 h-4 ml-2" />
                    </Link>
                  </Button>
                </CardContent>
              </Card>
            </div>
          </div>

          {/* Recent Activity & Quick Actions */}
          <div className="grid lg:grid-cols-3 gap-8">
            {/* Recent Activity */}
            <div className="lg:col-span-2">
              <Card className="border-0 bg-white/80 backdrop-blur-sm shadow-lg">
                <CardHeader>
                  <CardTitle className="flex items-center gap-2">
                    <Activity className="w-6 h-6 text-blue-600" />
                    Recent Activity
                  </CardTitle>
                  <CardDescription>Your latest AI agent interactions</CardDescription>
                </CardHeader>
                <CardContent className="space-y-4">
                  <div className="flex items-center gap-4 p-4 rounded-lg bg-gradient-to-r from-blue-50 to-purple-50 border border-blue-100">
                    <div className="w-10 h-10 rounded-full bg-gradient-to-br from-blue-500 to-purple-600 flex items-center justify-center">
                      <Bot className="w-5 h-5 text-white" />
                    </div>
                    <div className="flex-1">
                      <p className="font-medium text-slate-900">AI Agent "Code Assistant" deployed</p>
                      <p className="text-sm text-slate-600">2 hours ago • Automated code review</p>
                    </div>
                    <Badge className="bg-green-100 text-green-800">Active</Badge>
                  </div>

                  <div className="flex items-center gap-4 p-4 rounded-lg bg-gradient-to-r from-purple-50 to-pink-50 border border-purple-100">
                    <div className="w-10 h-10 rounded-full bg-gradient-to-br from-purple-500 to-pink-600 flex items-center justify-center">
                      <Brain className="w-5 h-5 text-white" />
                    </div>
                    <div className="flex-1">
                      <p className="font-medium text-slate-900">GPT-4 model integration completed</p>
                      <p className="text-sm text-slate-600">1 day ago • Enhanced response quality</p>
                    </div>
                    <Badge className="bg-blue-100 text-blue-800">Ready</Badge>
                  </div>

                  <div className="flex items-center gap-4 p-4 rounded-lg bg-gradient-to-r from-green-50 to-teal-50 border border-green-100">
                    <div className="w-10 h-10 rounded-full bg-gradient-to-br from-green-500 to-teal-600 flex items-center justify-center">
                      <Sparkles className="w-5 h-5 text-white" />
                    </div>
                    <div className="flex-1">
                      <p className="font-medium text-slate-900">New automation workflow created</p>
                      <p className="text-sm text-slate-600">3 days ago • Email processing pipeline</p>
                    </div>
                    <Badge className="bg-purple-100 text-purple-800">Live</Badge>
                  </div>
                </CardContent>
              </Card>
            </div>

            {/* Quick Actions */}
            <Card className="border-0 bg-white/80 backdrop-blur-sm shadow-lg">
              <CardHeader>
                <CardTitle className="flex items-center gap-2">
                  <Rocket className="w-6 h-6 text-orange-600" />
                  Quick Actions
                </CardTitle>
                <CardDescription>Jump into your most-used features</CardDescription>
              </CardHeader>
              <CardContent className="space-y-3">
                <Button
                  className="w-full justify-start bg-gradient-to-r from-blue-600 to-purple-600 hover:from-blue-700 hover:to-purple-700"
                  asChild
                >
                  <Link href="/agents">
                    <Bot className="w-4 h-4 mr-2" />
                    Deploy New Agent
                  </Link>
                </Button>

                <Button
                  variant="outline"
                  className="w-full justify-start border-purple-200 hover:bg-purple-50"
                  asChild
                >
                  <Link href="/models">
                    <Cpu className="w-4 h-4 mr-2" />
                    Add AI Model
                  </Link>
                </Button>

                <Button
                  variant="outline"
                  className="w-full justify-start border-green-200 hover:bg-green-50"
                  asChild
                >
                  <Link href="/tools">
                    <Settings className="w-4 h-4 mr-2" />
                    Configure Tools
                  </Link>
                </Button>

                {user?.role === 'admin' && (
                  <Button
                    variant="outline"
                    className="w-full justify-start border-orange-200 hover:bg-orange-50"
                    asChild
                  >
                    <Link href="/admin">
                      <Shield className="w-4 h-4 mr-2" />
                      Admin Panel
                    </Link>
                  </Button>
                )}
              </CardContent>
            </Card>
          </div>

          {/* Account Information */}
          <Card className="border-0 bg-gradient-to-br from-white to-slate-50 shadow-lg">
            <CardHeader
              className="cursor-pointer hover:bg-slate-50 transition-all duration-200"
              onClick={() => setIsAccountExpanded(!isAccountExpanded)}
            >
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-4">
                  <div className="w-12 h-12 rounded-full bg-gradient-to-br from-blue-500 to-purple-600 flex items-center justify-center text-white font-semibold text-lg">
                    {(user?.name || user?.email || 'U').charAt(0).toUpperCase()}
                  </div>
                  <div>
                    <CardTitle className="flex items-center gap-2 text-xl">
                      {user?.name || 'User Account'}
                      {isAccountExpanded ? (
                        <ChevronUp className="h-5 w-5 text-slate-500" />
                      ) : (
                        <ChevronDown className="h-5 w-5 text-slate-500" />
                      )}
                    </CardTitle>
                    <CardDescription className="flex items-center gap-2">
                      <Star className="w-4 h-4 text-yellow-500" />
                      {user?.role === 'admin' ? 'Administrator' : 'Standard User'} • {user?.email}
                    </CardDescription>
                  </div>
                </div>
              </div>
            </CardHeader>

            {isAccountExpanded && (
              <CardContent className="border-t bg-slate-50/50">
                <div className="grid md:grid-cols-2 gap-6">
                  <div className="space-y-4">
                    <div>
                      <label className="text-sm font-medium text-slate-600">Account Type</label>
                      <div className="mt-1 p-3 rounded-lg bg-white border">
                        <p className="font-semibold text-slate-900">
                          {user?.role === 'admin' ? 'Enterprise Administrator' : 'Standard Account'}
                        </p>
                        <p className="text-sm text-slate-600 mt-1">
                          {user?.role === 'admin'
                            ? 'Full access to all administrative features'
                            : 'Access to core AI agent functionality'
                          }
                        </p>
                      </div>
                    </div>

                    <div>
                      <label className="text-sm font-medium text-slate-600">Member Since</label>
                      <div className="mt-1 p-3 rounded-lg bg-white border">
                        <p className="font-semibold text-slate-900">
                          {user?.createdAt ? new Date(user.createdAt).toLocaleDateString() : 'Recently'}
                        </p>
                      </div>
                    </div>
                  </div>

                  <div className="space-y-4">
                    <div>
                      <label className="text-sm font-medium text-slate-600">Usage Statistics</label>
                      <div className="mt-1 p-3 rounded-lg bg-white border">
                        <div className="grid grid-cols-2 gap-4">
                          <div>
                            <p className="text-2xl font-bold text-blue-600">{apiUsage.totalRequests.toLocaleString()}</p>
                            <p className="text-sm text-slate-600">Total Requests</p>
                          </div>
                          <div>
                            <p className="text-2xl font-bold text-green-600">{apiUsage.todayRequests}</p>
                            <p className="text-sm text-slate-600">Today</p>
                          </div>
                        </div>
                      </div>
                    </div>

                    <div className="flex gap-3 pt-2">
                      <Button
                        variant="outline"
                        onClick={() => router.push('/admin/pricing')}
                        className="flex-1 border-blue-200 hover:bg-blue-50"
                      >
                        <BarChart3 className="w-4 h-4 mr-2" />
                        View Billing
                      </Button>
                      <Button
                        variant="outline"
                        onClick={handleLogout}
                        className="flex-1 border-red-200 hover:bg-red-50 text-red-600"
                      >
                        <ArrowRight className="w-4 h-4 mr-2 rotate-180" />
                        Logout
                      </Button>
                    </div>
                  </div>
                </div>
              </CardContent>
            )}
          </Card>
        </div>
      </div>
    </div>
  );
}
