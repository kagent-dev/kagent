'use server'

import { getSystemGrpcGateway } from '@/lib/grpc/client';
import { createErrorResponse } from './utils';
import { BaseResponse } from '@/types';

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
    const gateway = await getSystemGrpcGateway();
    const namespaces = await gateway.listNamespaces();

    return {
      message: "Namespaces fetched successfully",
      data: namespaces,
    };
  } catch (error) {
    return createErrorResponse<NamespaceResponse[]>(error, "Error getting namespaces");
  }
}
