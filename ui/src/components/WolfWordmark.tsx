"use client";

import * as React from "react";

interface WolfWordmarkProps extends React.SVGProps<SVGSVGElement> {
  title?: string;
}

// WolfWordmark — an improved intelligent wolf + embedded wordmark "alophe.ai"
// - Single SVG so glyph and text stay aligned and scale together
// - Uses currentColor for theme adaptivity
// - Geometric wolf head with clearer ears and muzzle; clean wordmark with subtle tracking
export default function WolfWordmark({ title = "alophe.ai", ...props }: WolfWordmarkProps) {
  return (
    <svg
      viewBox="0 0 340 64"
      xmlns="http://www.w3.org/2000/svg"
      aria-label={title}
      role="img"
      fill="none"
      {...props}
    >
      <title>{title}</title>
      {/* Wolf mark (left) */}
      <g stroke="currentColor" strokeWidth="2" strokeLinejoin="round" strokeLinecap="round">
        {/* Face shield */}
        <path d="M16 26c0-3.5 1.6-6.8 4.3-8.9l12-9.3c3.4-2.6 8.1-2.6 11.6 0l12 9.3c2.7 2.1 4.3 5.4 4.3 8.9v9.5c0 6.1-3.2 11.8-8.5 15.1l-9.8 6.1a4.6 4.6 0 0 1-4.9 0l-9.8-6.1A17.9 17.9 0 0 1 16 35.5V26Z"/>
        {/* Ears */}
        <path d="M26 18 L33 8 L35 19"/>
        <path d="M53 18 L46 8 L44 19"/>
        {/* Eyes */}
        <circle cx="30" cy="29" r="2" fill="currentColor" />
        <circle cx="48" cy="29" r="2" fill="currentColor" />
        {/* Brows (intelligent look) */}
        <path d="M26 24 C31 22, 35 22, 38 24"/>
        <path d="M38 24 C42 22, 46 22, 52 24"/>
        {/* Muzzle + nose */}
        <path d="M34 36 C40 38, 40 38, 44 36"/>
        <circle cx="39" cy="39" r="1.6" fill="currentColor" />
        {/* Jaw curve */}
        <path d="M28 42 C35 46, 43 46, 50 42"/>
      </g>

      {/* Wordmark "alophe.ai" (right). Rounded, friendly letterforms using path-based text for consistency */}
      <g transform="translate(80, 15)" fill="currentColor">
        {/* a */}
        <path d="M8 28c-4.5 0-8-3.1-8-7.7C0 15.2 3.4 12 8 12c2.2 0 3.9.7 5.2 2V12h4v16h-4v-1.7c-1.3 1.3-3 1.7-5.2 1.7Zm.7-3.5c3 0 4.5-1.9 4.5-4.2s-1.5-4.2-4.5-4.2-4.5 1.9-4.5 4.2 1.5 4.2 4.5 4.2Z"/>
        {/* l */}
        <path d="M24 28V6h4v22h-4Z"/>
        {/* o */}
        <path d="M36 28c-5 0-8.3-3.3-8.3-8s3.3-8 8.3-8 8.3 3.3 8.3 8-3.3 8-8.3 8Zm0-3.5c2.9 0 4.6-1.9 4.6-4.5s-1.7-4.5-4.6-4.5-4.6 1.9-4.6 4.5 1.7 4.5 4.6 4.5Z"/>
        {/* p */}
        <path d="M49 28V6h4v10.2c1.1-1.1 2.7-1.8 4.7-1.8 4 0 7 3 7 7.6S61.7 29 57.7 29c-2 0-3.7-.7-4.7-1.8V28h-4Zm8.7-3.5c2.7 0 4.4-1.8 4.4-4.5s-1.7-4.5-4.4-4.5-4.6 1.8-4.6 4.5 1.9 4.5 4.6 4.5Z"/>
        {/* h */}
        <path d="M79 28V12h4v7.1c0 3 1.7 5 4.7 5 2.7 0 4.6-1.9 4.6-5V12h4v10.7c0 5.1-3.4 6.9-7 6.9-2.3 0-4.2-.8-5.3-2.3V28h-5Z"/>
        {/* e */}
        <path d="M105.5 22.6c.5 2 2.3 2.9 4.5 2.9 1.8 0 3.4-.6 4.6-1.6l2 2.5c-1.8 1.6-4.3 2.6-7 2.6-5.1 0-8.6-3.1-8.6-8s3.7-8 8.3-8c4.5 0 7.9 3.3 7.9 8.1 0 .5 0 1-.1 1.5h-11.6Zm7.8-3c-.4-2-2.1-3.1-4-3.1s-3.7 1.2-4.2 3.1h8.2Z"/>
        {/* dot . */}
        <circle cx="125" cy="26" r="2"/>
        {/* a */}
        <path d="M137 28c-4.5 0-8-3.1-8-7.7 0-5.1 3.4-8.3 8-8.3 2.2 0 3.9.7 5.2 2V12h4v16h-4v-1.7c-1.3 1.3-3 1.7-5.2 1.7Zm.7-3.5c3 0 4.5-1.9 4.5-4.2s-1.5-4.2-4.5-4.2-4.5 1.9-4.5 4.2 1.5 4.2 4.5 4.2Z"/>
        {/* i */}
        <path d="M153 28V12h4v16h-4Zm0-18V6h4v4h-4Z"/>
      </g>
    </svg>
  );
}
