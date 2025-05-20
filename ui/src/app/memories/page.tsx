'use client'

import React, { useState, useEffect } from 'react'
import { useRouter } from 'next/navigation'
import { Plus, Trash2, Edit } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { MemoryResponse } from '@/lib/types'
import { listMemories, deleteMemory } from '../actions/memories'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { toast } from 'sonner'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog"

export default function MemoriesPage() {
  const router = useRouter()
  const [memories, setMemories] = useState<MemoryResponse[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const [isDeleteDialogOpen, setIsDeleteDialogOpen] = useState(false)
  const [memoryNameToDelete, setMemoryNameToDelete] = useState<string | null>(null)

  // Helper function to display not set for null/undefined/empty values
  const formatValue = (value: string | number | boolean | null | undefined): string => {
    return value === null || value === undefined || value === '' ? 'not set' : String(value);
  }

  useEffect(() => {
    async function loadMemories() {
      setLoading(true)
      setError(null)
      try {
        const data = await listMemories()
        setMemories(data)
      } catch (err) {
        const errorMessage = err instanceof Error ? err.message : 'An unknown error occurred'
        setError(errorMessage)
        toast.error(`Failed to load memories: ${errorMessage}`)
      } finally {
        setLoading(false)
      }
    }
    loadMemories()
  }, [])

  // Function to open the confirmation dialog
  const handleDeleteRequest = (name: string) => {
    setMemoryNameToDelete(name)
    setIsDeleteDialogOpen(true)
  }

  // Function to handle the actual deletion after confirmation
  const handleDeleteConfirm = async () => {
    if (!memoryNameToDelete) return // Should not happen if dialog is open

    const memoryToDelete = memoryNameToDelete;
    try {
      toast.info(`Deleting memory "${memoryToDelete}"...`)
      await deleteMemory(memoryToDelete)
      setMemories(memories.filter((m) => m.name !== memoryToDelete))
      toast.success(`Memory "${memoryToDelete}" deleted successfully.`)
    } catch (err) {
      const errorMessage = err instanceof Error ? err.message : 'An unknown error occurred'
      toast.error(`Failed to delete memory "${memoryToDelete}": ${errorMessage}`)
    } finally {
      setIsDeleteDialogOpen(false)
      setMemoryNameToDelete(null)
    }
  }

  return (
    <div className="container mx-auto py-10">
      <div className="flex justify-between items-center mb-6">
        <h1 className="text-3xl font-bold">Memories</h1>
        <Button onClick={() => router.push('/memories/new')}>
          <Plus className="h-5 w-5 mr-2" />
          New Memory
        </Button>
      </div>

      {loading && <p>Loading memories...</p>}
      {error && <p className="text-red-500">Error: {error}</p>}
      {!loading && !error && (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="w-[200px]">Name</TableHead>
              <TableHead>Provider</TableHead>
              <TableHead>Index Host</TableHead>
              <TableHead>Top K</TableHead>
              <TableHead>Namespace</TableHead>
              <TableHead>Record Fields</TableHead>
              <TableHead>Score Threshold</TableHead>
              <TableHead className="text-right">Actions</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {memories.length === 0 ? (
               <TableRow>
                 <TableCell colSpan={8} className="h-24 text-center">
                   No memories found.
                 </TableCell>
               </TableRow>
            ) : (
              memories.map((memory) => (
                <TableRow key={memory.name}>
                  <TableCell className="font-medium">{memory.name}</TableCell>
                  <TableCell>{formatValue(memory.providerName)}</TableCell>
                  <TableCell>{formatValue(memory.memoryParams?.indexHost)}</TableCell>
                  <TableCell>{formatValue(memory.memoryParams?.topK)}</TableCell>
                  <TableCell>{formatValue(memory.memoryParams?.namespace)}</TableCell>
                  <TableCell>{formatValue(memory.memoryParams?.recordFields)}</TableCell>
                  <TableCell>{formatValue(memory.memoryParams?.scoreThreshold)}</TableCell>
                  <TableCell className="text-right">
                    <Button
                      variant="ghost"
                      size="icon"
                      onClick={() => router.push(`/memories/new?edit=${encodeURIComponent(memory.name)}`)}
                      aria-label="Edit memory"
                      className="mr-1"
                    >
                      <Edit className="h-4 w-4" />
                    </Button>
                    <Button
                      variant="ghost"
                      size="icon"
                      onClick={() => handleDeleteRequest(memory.name)}
                      aria-label="Delete memory"
                    >
                      <Trash2 className="h-4 w-4" />
                    </Button>
                  </TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      )}

      {/* Delete Confirmation Dialog */}
      <AlertDialog open={isDeleteDialogOpen} onOpenChange={setIsDeleteDialogOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Are you absolutely sure?</AlertDialogTitle>
            <AlertDialogDescription>
              This action cannot be undone. This will permanently delete the
              memory configuration named &quot;<span className="font-semibold">{memoryNameToDelete}</span>&quot;.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction onClick={handleDeleteConfirm}>
              Delete
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
} 