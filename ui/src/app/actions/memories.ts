'use server'

import { MemoryResponse, CreateMemoryRequest, UpdateMemoryRequest } from '@/lib/types'
import { fetchApi } from './utils'

export async function listMemories(): Promise<MemoryResponse[]> {
  const data = await fetchApi<MemoryResponse[]>('/memories')
  return data.map(memory => ({
    ...memory,
    memoryParams: memory.memoryParams || {}
  }))
}

export async function getMemory(ref: string): Promise<MemoryResponse> {
  return fetchApi<MemoryResponse>(`/memories/${ref}`)
}

export async function createMemory(
  memoryData: CreateMemoryRequest
): Promise<MemoryResponse> {
  return fetchApi<MemoryResponse>('/memories', {
    method: 'POST',
    body: JSON.stringify(memoryData),
  })
}

export async function updateMemory(
  memoryData: UpdateMemoryRequest
): Promise<MemoryResponse> {
  return fetchApi<MemoryResponse>(`/memories/${memoryData.ref}`, {
    method: 'PUT',
    body: JSON.stringify(memoryData),
  })
}


export async function deleteMemory(ref: string): Promise<void> {
  await fetchApi<void>(`/memories/${ref}`, {
    method: 'DELETE',
  })
} 