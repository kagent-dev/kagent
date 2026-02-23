/**
 * Fetcher function for SWR
 * Handles API requests with proper error handling and BaseResponse unwrapping
 */
export const fetcher = async (url: string): Promise<unknown> => {
  const response = await fetch(url);
  
  if (!response.ok) {
    const error = new Error(`HTTP ${response.status}: ${response.statusText}`) as Error & {
      info?: unknown;
      status?: number;
    };
    // Attach extra info to the error object
    error.info = await response.json().catch(() => ({}));
    error.status = response.status;
    throw error;
  }
  
  const jsonData = await response.json();
  
  // If the response follows the BaseResponse format, return the data property
  // Otherwise, return the raw response
  if (jsonData && typeof jsonData === 'object' && 'data' in jsonData) {
    return jsonData.data;
  }
  
  return jsonData;
};

import { getBackendUrl } from '@/lib/utils';

/**
 * Build full API URL for a given endpoint using existing backend URL utility
 */
export const buildApiUrl = (endpoint: string): string => {
  const baseUrl = getBackendUrl();
  return `${baseUrl}${endpoint.startsWith('/') ? endpoint : `/${endpoint}`}`;
};

/**
 * Type-safe fetcher for specific data types
 */
export const createTypedFetcher = <T>() => {
  return async (url: string): Promise<T> => {
    const result = await fetcher(url);
    return result as T;
  };
};
