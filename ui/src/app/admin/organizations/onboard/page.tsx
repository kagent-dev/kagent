'use client';

import { useState } from 'react';
import { useRouter } from 'next/navigation';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Badge } from '@/components/ui/badge';
import { Textarea } from '@/components/ui/textarea';
import {
  Building2,
  Users,
  ArrowRight,
  CheckCircle,
  Sparkles,
  Shield,
  Globe,
  Mail,
  User,
  Loader2,
  ArrowLeft
} from 'lucide-react';
import { toast } from 'sonner';

interface OnboardingStep {
  id: number;
  title: string;
  description: string;
  completed: boolean;
}

export default function OrganizationOnboardingPage() {
  const router = useRouter();
  const [currentStep, setCurrentStep] = useState(1);
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState('');

  // Form data
  const [orgName, setOrgName] = useState('');
  const [orgDomain, setOrgDomain] = useState('');
  const [orgDescription, setOrgDescription] = useState('');
  const [adminName, setAdminName] = useState('');
  const [adminEmail, setAdminEmail] = useState('');
  const [adminPassword, setAdminPassword] = useState('');

  const steps: OnboardingStep[] = [
    {
      id: 1,
      title: 'Organization Details',
      description: 'Basic information about your organization',
      completed: currentStep > 1
    },
    {
      id: 2,
      title: 'Administrator Account',
      description: 'Create the first administrator account',
      completed: currentStep > 2
    },
    {
      id: 3,
      title: 'Review & Create',
      description: 'Review settings and create organization',
      completed: currentStep > 3
    }
  ];

  const handleNext = () => {
    if (currentStep < 3) {
      setCurrentStep(currentStep + 1);
    }
  };

  const handlePrevious = () => {
    if (currentStep > 1) {
      setCurrentStep(currentStep - 1);
    }
  };

  const handleSubmit = async () => {
    setIsLoading(true);
    setError('');

    try {
      const organizationData = {
        name: orgName,
        domain: orgDomain,
        description: orgDescription,
        admin: {
          name: adminName,
          email: adminEmail,
          password: adminPassword
        }
      };

      const res = await fetch('/api/admin/organizations', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Authorization': `Bearer ${localStorage.getItem('auth_token')}`
        },
        body: JSON.stringify(organizationData)
      });

      if (!res.ok) {
        const errorData = await res.json();
        throw new Error(errorData.error || 'Failed to create organization');
      }

      toast.success('Organization created successfully!');
      router.push('/admin/organizations');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create organization');
    } finally {
      setIsLoading(false);
    }
  };

  const validateStep = (step: number) => {
    switch (step) {
      case 1:
        return orgName.trim() && orgDomain.trim();
      case 2:
        return adminName.trim() && adminEmail.trim() && adminPassword.length >= 8;
      case 3:
        return true;
      default:
        return false;
    }
  };

  const renderStepIndicator = () => (
    <div className="flex items-center justify-center mb-8">
      {steps.map((step, index) => (
        <div key={step.id} className="flex items-center">
          <div className={`w-10 h-10 rounded-full flex items-center justify-center text-sm font-semibold transition-colors ${
            step.id === currentStep
              ? 'bg-blue-600 text-white'
              : step.completed
                ? 'bg-green-600 text-white'
                : 'bg-slate-200 text-slate-600'
          }`}>
            {step.completed ? <CheckCircle className="w-5 h-5" /> : step.id}
          </div>
          {index < steps.length - 1 && (
            <div className={`w-12 h-1 mx-2 transition-colors ${
              step.completed ? 'bg-green-600' : 'bg-slate-200'
            }`} />
          )}
        </div>
      ))}
    </div>
  );

  const renderStepContent = () => {
    switch (currentStep) {
      case 1:
        return (
          <div className="space-y-6">
            <div className="text-center mb-6">
              <div className="w-16 h-16 rounded-2xl bg-gradient-to-br from-blue-600 to-purple-600 flex items-center justify-center mx-auto mb-4">
                <Building2 className="w-8 h-8 text-white" />
              </div>
              <h2 className="text-2xl font-bold text-slate-900">Organization Details</h2>
              <p className="text-slate-600">Tell us about your organization</p>
            </div>

            <div className="space-y-4">
              <div>
                <Label htmlFor="orgName" className="text-sm font-medium text-slate-700">
                  Organization Name *
                </Label>
                <Input
                  id="orgName"
                  value={orgName}
                  onChange={(e) => setOrgName(e.target.value)}
                  placeholder="Acme Corporation"
                  className="mt-1"
                />
              </div>

              <div>
                <Label htmlFor="orgDomain" className="text-sm font-medium text-slate-700">
                  Domain *
                </Label>
                <Input
                  id="orgDomain"
                  value={orgDomain}
                  onChange={(e) => setOrgDomain(e.target.value)}
                  placeholder="acme.com"
                  className="mt-1"
                />
                <p className="text-xs text-slate-500 mt-1">Used for email routing and organization identification</p>
              </div>

              <div>
                <Label htmlFor="orgDescription" className="text-sm font-medium text-slate-700">
                  Description
                </Label>
                <Textarea
                  id="orgDescription"
                  value={orgDescription}
                  onChange={(e) => setOrgDescription(e.target.value)}
                  placeholder="Brief description of your organization..."
                  rows={3}
                  className="mt-1"
                />
              </div>
            </div>
          </div>
        );

      case 2:
        return (
          <div className="space-y-6">
            <div className="text-center mb-6">
              <div className="w-16 h-16 rounded-2xl bg-gradient-to-br from-purple-600 to-pink-600 flex items-center justify-center mx-auto mb-4">
                <Shield className="w-8 h-8 text-white" />
              </div>
              <h2 className="text-2xl font-bold text-slate-900">Administrator Account</h2>
              <p className="text-slate-600">Create the first organization administrator</p>
            </div>

            <div className="space-y-4">
              <div>
                <Label htmlFor="adminName" className="text-sm font-medium text-slate-700">
                  Administrator Name *
                </Label>
                <Input
                  id="adminName"
                  value={adminName}
                  onChange={(e) => setAdminName(e.target.value)}
                  placeholder="John Smith"
                  className="mt-1"
                />
              </div>

              <div>
                <Label htmlFor="adminEmail" className="text-sm font-medium text-slate-700">
                  Administrator Email *
                </Label>
                <Input
                  id="adminEmail"
                  type="email"
                  value={adminEmail}
                  onChange={(e) => setAdminEmail(e.target.value)}
                  placeholder="admin@acme.com"
                  className="mt-1"
                />
              </div>

              <div>
                <Label htmlFor="adminPassword" className="text-sm font-medium text-slate-700">
                  Administrator Password *
                </Label>
                <Input
                  id="adminPassword"
                  type="password"
                  value={adminPassword}
                  onChange={(e) => setAdminPassword(e.target.value)}
                  placeholder="Minimum 8 characters"
                  className="mt-1"
                />
                <p className="text-xs text-slate-500 mt-1">Must be at least 8 characters long</p>
              </div>
            </div>
          </div>
        );

      case 3:
        return (
          <div className="space-y-6">
            <div className="text-center mb-6">
              <div className="w-16 h-16 rounded-2xl bg-gradient-to-br from-green-600 to-teal-600 flex items-center justify-center mx-auto mb-4">
                <CheckCircle className="w-8 h-8 text-white" />
              </div>
              <h2 className="text-2xl font-bold text-slate-900">Review & Create</h2>
              <p className="text-slate-600">Review your organization settings</p>
            </div>

            <div className="bg-slate-50 rounded-lg p-6 space-y-4">
              <div>
                <h3 className="font-semibold text-slate-900 mb-2">Organization Information</h3>
                <div className="space-y-2 text-sm">
                  <div className="flex justify-between">
                    <span className="text-slate-600">Name:</span>
                    <span className="font-medium">{orgName}</span>
                  </div>
                  <div className="flex justify-between">
                    <span className="text-slate-600">Domain:</span>
                    <span className="font-medium">{orgDomain}</span>
                  </div>
                  {orgDescription && (
                    <div className="flex justify-between">
                      <span className="text-slate-600">Description:</span>
                      <span className="font-medium">{orgDescription}</span>
                    </div>
                  )}
                </div>
              </div>

              <div className="border-t pt-4">
                <h3 className="font-semibold text-slate-900 mb-2">Administrator Account</h3>
                <div className="space-y-2 text-sm">
                  <div className="flex justify-between">
                    <span className="text-slate-600">Name:</span>
                    <span className="font-medium">{adminName}</span>
                  </div>
                  <div className="flex justify-between">
                    <span className="text-slate-600">Email:</span>
                    <span className="font-medium">{adminEmail}</span>
                  </div>
                </div>
              </div>
            </div>

            <Alert>
              <CheckCircle className="h-4 w-4" />
              <AlertDescription>
                Your organization will be created with these settings. The administrator will receive an email invitation.
              </AlertDescription>
            </Alert>
          </div>
        );

      default:
        return null;
    }
  };

  return (
    <div className="min-h-screen bg-gradient-to-br from-slate-50 via-white to-blue-50">
      <div className="container mx-auto px-4 py-8">
        <div className="max-w-2xl mx-auto">
          {/* Header */}
          <div className="text-center mb-8">
            <div className="inline-flex items-center gap-2 px-4 py-2 rounded-full bg-gradient-to-r from-blue-100 to-purple-100 text-blue-800 text-sm font-medium mb-4">
              <Sparkles className="w-4 h-4" />
              Organization Setup
            </div>
            <h1 className="text-3xl font-bold bg-gradient-to-r from-slate-900 to-slate-700 bg-clip-text text-transparent">
              Create New Organization
            </h1>
            <p className="text-slate-600 mt-2">
              Set up a new organization with its own users and administrators
            </p>
          </div>

          {/* Step Indicator */}
          {renderStepIndicator()}

          <Card className="border-0 shadow-xl bg-white/80 backdrop-blur-sm">
            <CardContent className="p-8">
              {error && (
                <Alert variant="destructive" className="mb-6">
                  <AlertDescription>{error}</AlertDescription>
                </Alert>
              )}

              {renderStepContent()}

              {/* Navigation Buttons */}
              <div className="flex justify-between mt-8 pt-6 border-t">
                <Button
                  variant="outline"
                  onClick={handlePrevious}
                  disabled={currentStep === 1 || isLoading}
                >
                  <ArrowLeft className="w-4 h-4 mr-2" />
                  Previous
                </Button>

                {currentStep < 3 ? (
                  <Button
                    onClick={handleNext}
                    disabled={!validateStep(currentStep) || isLoading}
                    className="bg-gradient-to-r from-blue-600 to-purple-600 hover:from-blue-700 hover:to-purple-700"
                  >
                    Next
                    <ArrowRight className="w-4 h-4 ml-2" />
                  </Button>
                ) : (
                  <Button
                    onClick={handleSubmit}
                    disabled={!validateStep(currentStep) || isLoading}
                    className="bg-gradient-to-r from-green-600 to-teal-600 hover:from-green-700 hover:to-teal-700"
                  >
                    {isLoading ? (
                      <>
                        <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                        Creating Organization...
                      </>
                    ) : (
                      <>
                        <CheckCircle className="w-4 h-4 mr-2" />
                        Create Organization
                      </>
                    )}
                  </Button>
                )}
              </div>
            </CardContent>
          </Card>

          {/* Features */}
          <div className="mt-8 grid grid-cols-3 gap-4 text-center">
            <div className="flex flex-col items-center gap-2">
              <div className="w-8 h-8 rounded-full bg-blue-100 flex items-center justify-center">
                <Shield className="w-4 h-4 text-blue-600" />
              </div>
              <span className="text-xs text-slate-600">Secure</span>
            </div>
            <div className="flex flex-col items-center gap-2">
              <div className="w-8 h-8 rounded-full bg-purple-100 flex items-center justify-center">
                <Users className="w-4 h-4 text-purple-600" />
              </div>
              <span className="text-xs text-slate-600">Multi-tenant</span>
            </div>
            <div className="flex flex-col items-center gap-2">
              <div className="w-8 h-8 rounded-full bg-green-100 flex items-center justify-center">
                <Building2 className="w-4 h-4 text-green-600" />
              </div>
              <span className="text-xs text-slate-600">Scalable</span>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
