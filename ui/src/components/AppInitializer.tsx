"use client";

import React, { useState } from 'react';
import { OnboardingWizard } from './onboarding/OnboardingWizard';

const LOCAL_STORAGE_KEY = 'kagent-onboarding';

// Helper to safely read localStorage (returns null during SSR)
const getInitialOnboardingState = (): boolean | null => {
  if (typeof window === 'undefined') return null;
  const hasOnboarded = localStorage.getItem(LOCAL_STORAGE_KEY);
  return hasOnboarded !== 'true';
};

export function AppInitializer({ children }: { children: React.ReactNode }) {
  const [isOnboarding, setIsOnboarding] = useState<boolean | null>(getInitialOnboardingState);

  const handleOnboardingComplete = () => {
    localStorage.setItem(LOCAL_STORAGE_KEY, 'true');
    setIsOnboarding(false);
  };

  const handleSkipWizard = () => {
    localStorage.setItem(LOCAL_STORAGE_KEY, 'true');
    setIsOnboarding(false);
    // You might want to show a toast here as well, depending on your UI library setup
  };

  if (isOnboarding === null) {
    return null; 
  }

  if (isOnboarding) {
    return <OnboardingWizard onOnboardingComplete={handleOnboardingComplete} onSkip={handleSkipWizard} />;
  }

  return <>{children}</>;
} 