"use client";

import React, { useEffect, useState } from "react";
import { usePathname } from "next/navigation";
import { OnboardingWizard } from "./onboarding/OnboardingWizard";

const LOCAL_STORAGE_KEY = "kagent-onboarding";

export function AppInitializer({ children }: { children: React.ReactNode }) {
  /** `null` = not read yet (must match server + first client paint to avoid hydration mismatch) */
  const [isOnboarding, setIsOnboarding] = useState<boolean | null>(null);
  const pathname = usePathname();

  useEffect(() => {
    const hasOnboarded = localStorage.getItem(LOCAL_STORAGE_KEY) === "true";
    setIsOnboarding(!hasOnboarded);
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

  // Don't show the wizard on the login page
  if (isOnboarding && pathname !== '/login') {
    return <OnboardingWizard onOnboardingComplete={handleOnboardingComplete} onSkip={handleSkipWizard} />;
  }

  return <>{children}</>;
} 