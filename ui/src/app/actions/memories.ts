'use server'

import { BaseResponse, MemoryResponse, CreateMemoryRequest, UpdateMemoryRequest } from '@/lib/types'
import { fetchApi } from './utils'

export async function listMemories(): Promise<MemoryResponse[]> {
  const data = await fetchApi<BaseResponse<MemoryResponse[]>>('/memories')
  return data.data || []
}

export async function getMemory(ref: string): Promise<MemoryResponse> {
  const data = await fetchApi<BaseResponse<MemoryResponse>>(`/memories/${ref}`)
  return data.data || {} as MemoryResponse
}

export async function createMemory(
  memoryData: CreateMemoryRequest
): Promise<MemoryResponse> {
  const data = await fetchApi<BaseResponse<MemoryResponse>>('/memories', {
    method: 'POST',
    body: JSON.stringify(memoryData),
  })
  return data.data || {} as MemoryResponse
}

export async function updateMemory(
  memoryData: UpdateMemoryRequest
): Promise<MemoryResponse> {
  const data = await fetchApi<BaseResponse<MemoryResponse>>(`/memories/${memoryData.ref}`, {
    method: 'PUT',
    body: JSON.stringify(memoryData),
  })
  return data.data || {} as MemoryResponse
}


export async function deleteMemory(ref: string): Promise<void> {
  await fetchApi<void>(`/memories/${ref}`, {
    method: 'DELETE',
  })
} 