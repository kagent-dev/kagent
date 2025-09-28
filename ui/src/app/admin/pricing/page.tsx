'use client';

import { useState, useEffect } from 'react';
import { useAuth } from '@/hooks/useAuth';
import { useAgents } from '@/components/AgentsProvider';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import {
  CreditCard,
  Smartphone,
  Building2,
  CheckCircle,
  Star,
  Zap,
  Shield,
  Users,
  Database,
  Activity,
  DollarSign,
  Calendar,
  TrendingUp,
  AlertCircle
} from 'lucide-react';

interface PaymentMethod {
  id: string;
  type: 'credit_card' | 'paypal' | 'bank_transfer';
  name: string;
  icon: React.ReactNode;
  description: string;
  processingFee: number;
  processingTime: string;
}

interface BillingInfo {
  agents: {
    enabled: number;
    disabled: number;
    rate: number;
    total: number;
  };
  models: {
    active: number;
    rate: number;
    total: number;
  };
  apiUsage: {
    requests: number;
    rate: number;
    total: number;
    todayRequests: number;
    todayTotal: number;
  };
  total: number;
  currency: string;
}

export default function AdminPricingPage() {
  const { user, isAuthenticated } = useAuth();
  const { agents, models } = useAgents();
  const [enabledMap, setEnabledMap] = useState<Record<string, boolean>>({});
  const [selectedPaymentMethod, setSelectedPaymentMethod] = useState<string>('credit_card');
  const [apiUsage, setApiUsage] = useState({ totalRequests: 0, todayRequests: 0, lastUpdated: '' });

  // Payment methods configuration
  const paymentMethods: PaymentMethod[] = [
    {
      id: 'credit_card',
      type: 'credit_card',
      name: 'Credit Card',
      icon: <CreditCard className="w-6 h-6" />,
      description: 'Visa, Mastercard, American Express',
      processingFee: 2.9,
      processingTime: 'Instant'
    },
    {
      id: 'paypal',
      type: 'paypal',
      name: 'PayPal',
      icon: <Smartphone className="w-6 h-6" />,
      description: 'Pay with your PayPal account',
      processingFee: 3.4,
      processingTime: '1-2 minutes'
    },
    {
      id: 'bank_transfer',
      type: 'bank_transfer',
      name: 'Bank Transfer',
      icon: <Building2 className="w-6 h-6" />,
      description: 'Direct bank transfer (ACH)',
      processingFee: 0,
      processingTime: '1-3 business days'
    }
  ];

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

  // Fetch enabled/disabled agent status
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

  // Calculate real-time billing information
  const calculateBillingInfo = (): BillingInfo => {
    const totalAgents = agents?.length || 0;
    const enabledAgents = agents?.filter(a => {
      const ns = a.agent.metadata.namespace || '';
      const name = a.agent.metadata.name;
      const ref = `${ns}/${name}`;
      const flag = enabledMap.hasOwnProperty(ref) ? enabledMap[ref] : true;
      return flag;
    }).length || 0;

    const disabledAgents = totalAgents - enabledAgents;
    const totalModels = models?.length || 0;
    const activeModels = totalModels;

    return {
      agents: {
        enabled: enabledAgents,
        disabled: disabledAgents,
        rate: 9.99,
        total: enabledAgents * 9.99
      },
      models: {
        active: activeModels,
        rate: 4.99,
        total: activeModels * 4.99
      },
      apiUsage: {
        requests: apiUsage.totalRequests,
        rate: 0.001,
        total: apiUsage.totalRequests * 0.001,
        todayRequests: apiUsage.todayRequests,
        todayTotal: apiUsage.todayRequests * 0.001
      },
      total: (enabledAgents * 9.99) + (activeModels * 4.99) + (apiUsage.totalRequests * 0.001),
      currency: 'USD'
    };
  };

  const billingInfo = calculateBillingInfo();
  const selectedMethod = paymentMethods.find(m => m.id === selectedPaymentMethod);

  if (!isAuthenticated) {
    return (
      <div className="min-h-screen bg-gradient-to-br from-background via-background to-muted/20">
        <div className="container mx-auto px-4 py-16">
          <div className="text-center">
            <AlertCircle className="w-16 h-16 text-muted-foreground mx-auto mb-4" />
            <h1 className="text-2xl font-bold mb-2">Authentication Required</h1>
            <p className="text-muted-foreground">Please log in to access billing information.</p>
          </div>
        </div>
      </div>
    );
  }

  if (user?.role !== 'admin') {
    return (
      <div className="min-h-screen bg-gradient-to-br from-background via-background to-muted/20">
        <div className="container mx-auto px-4 py-16">
          <div className="text-center">
            <AlertCircle className="w-16 h-16 text-muted-foreground mx-auto mb-4" />
            <h1 className="text-2xl font-bold mb-2">Access Denied</h1>
            <p className="text-muted-foreground">Admin privileges required to access billing information.</p>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="min-h-screen bg-gradient-to-br from-background via-background to-muted/20">
      <div className="container mx-auto px-4 py-8">
        <div className="max-w-6xl mx-auto space-y-8">
          {/* Header */}
          <div className="text-center space-y-4">
            <div className="inline-flex items-center gap-2 px-4 py-2 rounded-full bg-gradient-to-r from-primary/10 to-primary/5 text-primary text-sm font-medium border border-primary/20">
              <DollarSign className="w-4 h-4" />
              Billing & Payment Management
            </div>
            <h1 className="text-4xl md:text-5xl font-bold bg-gradient-to-r from-foreground to-muted-foreground bg-clip-text text-transparent">
              Enterprise Billing Dashboard
            </h1>
            <p className="text-lg text-muted-foreground max-w-2xl mx-auto">
              Monitor costs, manage subscriptions, and handle payments for your AI agent platform.
              Real-time usage tracking with multiple payment options.
            </p>
          </div>

          {/* Current Usage & Billing Overview */}
          <div className="grid md:grid-cols-2 lg:grid-cols-4 gap-6">
            {/* Agents Billing Card */}
            <Card className="group hover:shadow-lg transition-all duration-300 border-0 bg-gradient-to-br from-card to-card/50 hover:from-blue-500/5 hover:to-blue-500/10">
              <CardHeader className="pb-3">
                <div className="flex items-center gap-3">
                  <div className="w-12 h-12 rounded-xl bg-blue-500/10 flex items-center justify-center group-hover:bg-blue-500/20 transition-colors">
                    <Users className="w-6 h-6 text-blue-600" />
                  </div>
                  <div>
                    <CardTitle className="text-lg">Agent Charges</CardTitle>
                    <div className="text-2xl font-bold text-blue-600">${billingInfo.agents.total.toFixed(2)}</div>
                  </div>
                </div>
              </CardHeader>
              <CardContent className="space-y-2">
                <div className="flex justify-between text-sm">
                  <span className="text-muted-foreground">Enabled Agents:</span>
                  <span className="font-medium">{billingInfo.agents.enabled}</span>
                </div>
                <div className="flex justify-between text-sm">
                  <span className="text-muted-foreground">Rate:</span>
                  <span className="font-medium">${billingInfo.agents.rate}/month</span>
                </div>
                <div className="flex justify-between text-sm">
                  <span className="text-muted-foreground">Disabled:</span>
                  <span className="text-muted-foreground">{billingInfo.agents.disabled}</span>
                </div>
              </CardContent>
            </Card>

            {/* Models Billing Card */}
            <Card className="group hover:shadow-lg transition-all duration-300 border-0 bg-gradient-to-br from-card to-card/50 hover:from-purple-500/5 hover:to-purple-500/10">
              <CardHeader className="pb-3">
                <div className="flex items-center gap-3">
                  <div className="w-12 h-12 rounded-xl bg-purple-500/10 flex items-center justify-center group-hover:bg-purple-500/20 transition-colors">
                    <Database className="w-6 h-6 text-purple-600" />
                  </div>
                  <div>
                    <CardTitle className="text-lg">Model Charges</CardTitle>
                    <div className="text-2xl font-bold text-purple-600">${billingInfo.models.total.toFixed(2)}</div>
                  </div>
                </div>
              </CardHeader>
              <CardContent className="space-y-2">
                <div className="flex justify-between text-sm">
                  <span className="text-muted-foreground">Active Models:</span>
                  <span className="font-medium">{billingInfo.models.active}</span>
                </div>
                <div className="flex justify-between text-sm">
                  <span className="text-muted-foreground">Rate:</span>
                  <span className="font-medium">${billingInfo.models.rate}/month</span>
                </div>
              </CardContent>
            </Card>

            {/* API Usage Billing Card */}
            <Card className="group hover:shadow-lg transition-all duration-300 border-0 bg-gradient-to-br from-card to-card/50 hover:from-green-500/5 hover:to-green-500/10">
              <CardHeader className="pb-3">
                <div className="flex items-center gap-3">
                  <div className="w-12 h-12 rounded-xl bg-green-500/10 flex items-center justify-center group-hover:bg-green-500/20 transition-colors">
                    <Activity className="w-6 h-6 text-green-600" />
                  </div>
                  <div>
                    <CardTitle className="text-lg">API Usage</CardTitle>
                    <div className="text-2xl font-bold text-green-600">${billingInfo.apiUsage.total.toFixed(3)}</div>
                  </div>
                </div>
              </CardHeader>
              <CardContent className="space-y-2">
                <div className="flex justify-between text-sm">
                  <span className="text-muted-foreground">Total Requests:</span>
                  <span className="font-medium">{billingInfo.apiUsage.requests.toLocaleString()}</span>
                </div>
                <div className="flex justify-between text-sm">
                  <span className="text-muted-foreground">Rate:</span>
                  <span className="font-medium">${billingInfo.apiUsage.rate}/request</span>
                </div>
                <div className="flex justify-between text-sm">
                  <span className="text-muted-foreground">Today:</span>
                  <span className="font-medium">{billingInfo.apiUsage.todayRequests}</span>
                </div>
              </CardContent>
            </Card>

            {/* Total Billing Card */}
            <Card className="group hover:shadow-lg transition-all duration-300 border-2 border-primary/20 bg-gradient-to-br from-primary/5 to-primary/10">
              <CardHeader className="pb-3">
                <div className="flex items-center gap-3">
                  <div className="w-12 h-12 rounded-xl bg-primary/10 flex items-center justify-center group-hover:bg-primary/20 transition-colors">
                    <TrendingUp className="w-6 h-6 text-primary" />
                  </div>
                  <div>
                    <CardTitle className="text-lg">Monthly Total</CardTitle>
                    <div className="text-2xl font-bold text-primary">${billingInfo.total.toFixed(2)}</div>
                  </div>
                </div>
              </CardHeader>
              <CardContent>
                <div className="flex items-center gap-2">
                  <Badge variant="secondary" className="bg-green-100 text-green-800">
                    <CheckCircle className="w-3 h-3 mr-1" />
                    Active
                  </Badge>
                  <span className="text-sm text-muted-foreground">Updated in real-time</span>
                </div>
              </CardContent>
            </Card>
          </div>

          {/* Payment Methods Section */}
          <div className="space-y-6">
            <div className="text-center">
              <h2 className="text-3xl font-bold bg-gradient-to-r from-foreground to-muted-foreground bg-clip-text text-transparent">
                Payment Methods
              </h2>
              <p className="text-muted-foreground">Choose your preferred payment method</p>
            </div>

            <div className="grid md:grid-cols-3 gap-6">
              {paymentMethods.map((method) => (
                <Card
                  key={method.id}
                  className={`cursor-pointer transition-all duration-300 border-2 hover:shadow-lg ${
                    selectedPaymentMethod === method.id
                      ? 'border-primary bg-primary/5'
                      : 'border-border hover:border-primary/50'
                  }`}
                  onClick={() => setSelectedPaymentMethod(method.id)}
                >
                  <CardHeader className="text-center">
                    <div className="w-16 h-16 rounded-full bg-gradient-to-br from-primary/10 to-primary/5 flex items-center justify-center mx-auto mb-4">
                      <div className="text-primary">
                        {method.icon}
                      </div>
                    </div>
                    <CardTitle className="flex items-center justify-center gap-2">
                      {method.name}
                      {selectedPaymentMethod === method.id && (
                        <CheckCircle className="w-5 h-5 text-primary" />
                      )}
                    </CardTitle>
                    <CardDescription>{method.description}</CardDescription>
                  </CardHeader>
                  <CardContent className="space-y-4">
                    <div className="flex justify-between text-sm">
                      <span className="text-muted-foreground">Processing Fee:</span>
                      <span className="font-medium">{method.processingFee}%</span>
                    </div>
                    <div className="flex justify-between text-sm">
                      <span className="text-muted-foreground">Processing Time:</span>
                      <span className="font-medium">{method.processingTime}</span>
                    </div>
                    <div className="pt-2 border-t">
                      <div className="text-center">
                        <div className="text-sm text-muted-foreground">Total with fees</div>
                        <div className="text-xl font-bold text-primary">
                          ${(billingInfo.total * (1 + method.processingFee / 100)).toFixed(2)}
                        </div>
                      </div>
                    </div>
                  </CardContent>
                </Card>
              ))}
            </div>
          </div>

          {/* Payment Summary & Action */}
          <Card className="border-0 bg-gradient-to-br from-card to-card/50 shadow-lg">
            <CardHeader>
              <CardTitle className="flex items-center gap-2 text-xl">
                <DollarSign className="w-6 h-6 text-primary" />
                Payment Summary
              </CardTitle>
              <CardDescription>
                Review your charges and complete payment
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-6">
              <div className="grid md:grid-cols-2 gap-6">
                <div className="space-y-4">
                  <h3 className="font-semibold text-lg">Billing Breakdown</h3>
                  <div className="space-y-3">
                    <div className="flex justify-between items-center p-3 rounded-lg bg-muted/30">
                      <div className="flex items-center gap-2">
                        <Users className="w-4 h-4 text-blue-600" />
                        <span>Enabled Agents</span>
                      </div>
                      <span className="font-medium">${billingInfo.agents.total.toFixed(2)}</span>
                    </div>
                    <div className="flex justify-between items-center p-3 rounded-lg bg-muted/30">
                      <div className="flex items-center gap-2">
                        <Database className="w-4 h-4 text-purple-600" />
                        <span>Active Models</span>
                      </div>
                      <span className="font-medium">${billingInfo.models.total.toFixed(2)}</span>
                    </div>
                    <div className="flex justify-between items-center p-3 rounded-lg bg-muted/30">
                      <div className="flex items-center gap-2">
                        <Activity className="w-4 h-4 text-green-600" />
                        <span>API Usage</span>
                      </div>
                      <span className="font-medium">${billingInfo.apiUsage.total.toFixed(3)}</span>
                    </div>
                  </div>
                </div>

                <div className="space-y-4">
                  <h3 className="font-semibold text-lg">Payment Details</h3>
                  <div className="space-y-3">
                    <div className="flex justify-between">
                      <span className="text-muted-foreground">Subtotal:</span>
                      <span className="font-medium">${billingInfo.total.toFixed(2)}</span>
                    </div>
                    <div className="flex justify-between">
                      <span className="text-muted-foreground">Processing Fee:</span>
                      <span className="font-medium">
                        {selectedMethod ? `${selectedMethod.processingFee}%` : '0%'}
                      </span>
                    </div>
                    <div className="flex justify-between text-lg font-semibold border-t pt-2">
                      <span>Total:</span>
                      <span className="text-primary">
                        ${selectedMethod ? (billingInfo.total * (1 + selectedMethod.processingFee / 100)).toFixed(2) : billingInfo.total.toFixed(2)}
                      </span>
                    </div>
                  </div>

                  <div className="flex items-center gap-2 p-3 rounded-lg bg-blue-50 border border-blue-200">
                    <Calendar className="w-4 h-4 text-blue-600" />
                    <span className="text-sm text-blue-800">
                      Next billing date: {new Date(Date.now() + 30 * 24 * 60 * 60 * 1000).toLocaleDateString()}
                    </span>
                  </div>
                </div>
              </div>

              <div className="flex gap-4 pt-4 border-t">
                <Button
                  className="flex-1 bg-gradient-to-r from-primary to-primary/80 hover:from-primary/90 hover:to-primary/70"
                  size="lg"
                >
                  <CreditCard className="w-4 h-4 mr-2" />
                  Complete Payment
                </Button>
                <Button variant="outline" size="lg">
                  <Zap className="w-4 h-4 mr-2" />
                  Auto-Renew
                </Button>
              </div>
            </CardContent>
          </Card>

          {/* Pricing Plans */}
          <div className="space-y-6">
            <div className="text-center">
              <h2 className="text-3xl font-bold bg-gradient-to-r from-foreground to-muted-foreground bg-clip-text text-transparent">
                Pricing Plans
              </h2>
              <p className="text-muted-foreground">Choose the plan that fits your needs</p>
            </div>

            <div className="grid md:grid-cols-3 gap-6">
              <Card className="border-2 border-muted hover:border-primary/50 transition-colors">
                <CardHeader className="text-center">
                  <CardTitle className="text-xl">Starter</CardTitle>
                  <div className="text-3xl font-bold text-primary">$29<span className="text-lg text-muted-foreground">/month</span></div>
                  <CardDescription>Perfect for small teams</CardDescription>
                </CardHeader>
                <CardContent className="space-y-4">
                  <div className="space-y-2">
                    <div className="flex items-center gap-2">
                      <CheckCircle className="w-4 h-4 text-green-600" />
                      <span className="text-sm">Up to 3 agents</span>
                    </div>
                    <div className="flex items-center gap-2">
                      <CheckCircle className="w-4 h-4 text-green-600" />
                      <span className="text-sm">2 model configurations</span>
                    </div>
                    <div className="flex items-center gap-2">
                      <CheckCircle className="w-4 h-4 text-green-600" />
                      <span className="text-sm">10,000 API requests/month</span>
                    </div>
                    <div className="flex items-center gap-2">
                      <CheckCircle className="w-4 h-4 text-green-600" />
                      <span className="text-sm">Email support</span>
                    </div>
                  </div>
                  <Button className="w-full" variant="outline">Current Plan</Button>
                </CardContent>
              </Card>

              <Card className="border-2 border-primary bg-gradient-to-br from-primary/5 to-primary/10 relative">
                <div className="absolute -top-3 left-1/2 transform -translate-x-1/2">
                  <Badge className="bg-primary text-primary-foreground">Most Popular</Badge>
                </div>
                <CardHeader className="text-center">
                  <CardTitle className="text-xl">Professional</CardTitle>
                  <div className="text-3xl font-bold text-primary">$99<span className="text-lg text-muted-foreground">/month</span></div>
                  <CardDescription>For growing businesses</CardDescription>
                </CardHeader>
                <CardContent className="space-y-4">
                  <div className="space-y-2">
                    <div className="flex items-center gap-2">
                      <CheckCircle className="w-4 h-4 text-green-600" />
                      <span className="text-sm">Up to 10 agents</span>
                    </div>
                    <div className="flex items-center gap-2">
                      <CheckCircle className="w-4 h-4 text-green-600" />
                      <span className="text-sm">5 model configurations</span>
                    </div>
                    <div className="flex items-center gap-2">
                      <CheckCircle className="w-4 h-4 text-green-600" />
                      <span className="text-sm">50,000 API requests/month</span>
                    </div>
                    <div className="flex items-center gap-2">
                      <CheckCircle className="w-4 h-4 text-green-600" />
                      <span className="text-sm">Priority support</span>
                    </div>
                    <div className="flex items-center gap-2">
                      <CheckCircle className="w-4 h-4 text-green-600" />
                      <span className="text-sm">Advanced analytics</span>
                    </div>
                  </div>
                  <Button className="w-full bg-gradient-to-r from-primary to-primary/80">Upgrade</Button>
                </CardContent>
              </Card>

              <Card className="border-2 border-muted hover:border-primary/50 transition-colors">
                <CardHeader className="text-center">
                  <CardTitle className="text-xl">Enterprise</CardTitle>
                  <div className="text-3xl font-bold text-primary">$299<span className="text-lg text-muted-foreground">/month</span></div>
                  <CardDescription>For large organizations</CardDescription>
                </CardHeader>
                <CardContent className="space-y-4">
                  <div className="space-y-2">
                    <div className="flex items-center gap-2">
                      <CheckCircle className="w-4 h-4 text-green-600" />
                      <span className="text-sm">Unlimited agents</span>
                    </div>
                    <div className="flex items-center gap-2">
                      <CheckCircle className="w-4 h-4 text-green-600" />
                      <span className="text-sm">Unlimited models</span>
                    </div>
                    <div className="flex items-center gap-2">
                      <CheckCircle className="w-4 h-4 text-green-600" />
                      <span className="text-sm">Unlimited API requests</span>
                    </div>
                    <div className="flex items-center gap-2">
                      <CheckCircle className="w-4 h-4 text-green-600" />
                      <span className="text-sm">24/7 phone support</span>
                    </div>
                    <div className="flex items-center gap-2">
                      <CheckCircle className="w-4 h-4 text-green-600" />
                      <span className="text-sm">Custom integrations</span>
                    </div>
                  </div>
                  <Button className="w-full" variant="outline">Contact Sales</Button>
                </CardContent>
              </Card>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
