'use server'

import { fetchApi, createErrorResponse } from './utils';
import { BaseResponse } from '@/lib/types';

// TODO(infocus7): move to datamodel or another type file
export interface NamespaceResponse {
  name: string;
  status: string;
}

/**
 * Lists all available namespaces
 * @returns A promise with the list of namespaces
 */
export async function listNamespaces(): Promise<BaseResponse<NamespaceResponse[]>> {
  try {
    const response = await fetchApi<BaseResponse<NamespaceResponse[]>>('/namespaces');
    
    if (!response) {
      throw new Error("Failed to get namespaces");
    }

    return {
      message: "Namespaces fetched successfully",
      data: response.data,
    };
  } catch (error) {
    return createErrorResponse<NamespaceResponse[]>(error, "Error getting namespaces");
  }
}