"use client";
import React from "react";
import KagentLogo from "./kagent-logo";

export function LoadingState() {

  return (
    <div>
      <div className="fixed inset-0 w-full h-full flex flex-col items-center justify-center backdrop-blur-sm bg-black/30 z-10">
        <div className="absolute opacity-20 animate-pulse">
          <KagentLogo className="w-32 h-32" />
        </div>
      </div>
    </div>
  );
}
