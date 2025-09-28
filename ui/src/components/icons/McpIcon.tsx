import React from 'react';

const McpIcon: React.FC<{ className?: string }> = ({ className }) => (
  <svg 
    width="16" 
    height="16" 
    viewBox="0 0 156 174" 
    fill="none" 
    xmlns="http://www.w3.org/2000/svg" 
    className={className}
    stroke="currentColor" // Use currentColor for easier styling
    strokeWidth="12" 
    strokeLinecap="round"
  >
    <path d="M6 81.8528L73.8823 13.9706C83.255 4.598 98.451 4.598 107.823 13.9706C117.196 23.3431 117.196 38.5391 107.823 47.9117L56.5581 99.177" />
    <path d="M57.2653 98.47L107.823 47.9117C117.196 38.5391 132.392 38.5391 141.765 47.9117L142.118 48.2652C151.491 57.6378 151.491 72.8338 142.118 82.2063L80.7248 143.6C77.6006 146.724 77.6006 151.789 80.7248 154.913L93.331 167.52" />
    <path d="M90.8529 30.9411L40.6481 81.1457C31.2756 90.518 31.2756 105.714 40.6481 115.087C50.0207 124.459 65.2168 124.459 74.5894 115.087L124.794 64.8822" />
  </svg>
);

export default McpIcon; 