import React from "react";

export default function KagentLogo({ className, animate = false }: { className?: string; animate?: boolean }) {
  const blinkStyles = `
    @keyframes blinkRight {
      0%, 95%, 100% {
        opacity: 1;
      }
      96%, 99% {
        opacity: 0;
      }
    }
    
    @keyframes blinkLeft {
      0%, 96%, 100% {
        opacity: 1;
      }
      97%, 99% {
        opacity: 0;
      }
    }
  `;

  const rightEyeStyle = animate
    ? {
        animation: "blinkRight 10s ease-in-out infinite",
      }
    : {};

  const leftEyeStyle = animate
    ? {
        animation: "blinkLeft 10s ease-in-out infinite",
      }
    : {};

  return (
    <>
      <style>{blinkStyles}</style>
      <svg width={378} height={286} viewBox="0 0 378 286" fill="none" xmlns="http://www.w3.org/2000/svg" className={className}>
        <path 
          d="M283.198 143.037H236.099V190.438H283.198V143.037Z" 
          fill="#942DE7" 
          style={rightEyeStyle} 
        />
        <path 
          d="M189.074 143.037H141.975V190.438H189.074V143.037Z" 
          fill="#942DE7" 
          style={leftEyeStyle} 
        />
        <path
          d="M330.223 48.3099L283.124 0.90918H236.099H189H141.975H94.8759V48.3099H47.8507V95.6364H0.75177V143.037V190.364L47.8507 237.764L94.8759 285.091H141.975H189H236.099H283.124H330.223V237.764H283.124H236.099H189H141.975H94.8759V190.364V143.037V95.6364H141.975H189H236.099H283.124H330.223V143.037V190.364V237.764H377.248V190.364V143.037V95.6364L330.223 48.3099Z"
          fill="#942DE7"
        />
      </svg>
    </>
  );
}