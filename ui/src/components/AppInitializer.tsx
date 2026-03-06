"use client";

import React, { useEffect, useState } from 'react';
import { OnboardingWizard } from './onboarding/OnboardingWizard';

const LOCAL_STORAGE_KEY = 'kagent-onboarding';

export function AppInitializer({ children }: { children: React.ReactNode }) {
  const [isOnboarding, setIsOnboarding] = useState<boolean | null>(null);

  useEffect(() => {
    const hasOnboarded = localStorage.getItem(LOCAL_STORAGE_KEY);
    setIsOnboarding(hasOnboarded !== 'true');
  }, []);

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