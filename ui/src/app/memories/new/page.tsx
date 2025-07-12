'use client'

import React, { useState, useEffect } from 'react'
import { useRouter, useSearchParams } from 'next/navigation'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import * as z from 'zod'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from "@/components/ui/form"
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"

import { getSupportedMemoryProviders } from '@/app/actions/providers'
import { createMemory, getMemory, updateMemory } from '@/app/actions/memories'
import { Provider, CreateMemoryRequest, PineconeConfigPayload } from '@/lib/types'
import { k8sRefUtils } from '@/lib/k8sUtils'
import { NamespaceCombobox } from '@/components/NamespaceCombobox'

// Base schema
const baseFormSchema = z.object({
  name: z.string().min(1, "Name is required"),
  namespace: z.string().optional(),
  providerType: z.string().min(1, "Provider is required"),
  apiKey: z.string().min(1, "API Key is required").or(z.literal('')), // Allow empty API key in edit mode
  // Generic object to hold dynamic provider parameters
  providerParams: z.record(z.string(), z.any()).optional(),
})

// Function to create a refined schema based on selected provider
const createRefinedSchema = (selectedProvider: Provider | null, isEditing: boolean = false) => {
  return baseFormSchema.refine((data) => {
    // Skip API key validation in edit mode
    if (isEditing && data.apiKey === '') return true;
    
    if (!selectedProvider || !data.providerParams) return true;
    for (const param of selectedProvider.requiredParams) {
      if (data.providerParams[param] === undefined || data.providerParams[param] === '') {
        return false;
      }
    }
    return true;
  }, {
    message: "Please fill in all required provider parameters.",
  });
};

export default function NewMemoryPage() {
  const router = useRouter()
  const searchParams = useSearchParams()
  const editMode = searchParams.get('edit') === "true"; 
  const memoryNameToEdit = searchParams.get('name')
  const memoryNamespaceToEdit = searchParams.get('namespace')

  const [providers, setProviders] = useState<Provider[]>([])
  const [isLoading, setIsLoading] = useState(false)
  const [selectedProvider, setSelectedProvider] = useState<Provider | null>(null)
  const [currentSchema, setCurrentSchema] = useState(createRefinedSchema(null, editMode));

  // Parameter labels for human-readable field names
  const paramLabels: Record<string, string> = {
    topK: "Top Results",
    scoreThreshold: "Similarity Threshold",
    indexHost: "Index Host URL",
    namespace: "Namespace",
    recordFields: "Record Fields",
  };

  // Parameter descriptions for better UX
  const paramDescriptions: Record<string, string> = {
    topK: "Number of top results to return from the database",
    scoreThreshold: "Minimum similarity score (0.0-1.0) for returned results",
    indexHost: "The host URL for your database",
    namespace: "Optional namespace",
    recordFields: "Comma-separated list of fields to include in query results",
  };

  useEffect(() => {
    setCurrentSchema(createRefinedSchema(selectedProvider, editMode));
  }, [selectedProvider, editMode]);

  type MemoryFormValues = z.infer<typeof baseFormSchema>;

  const form = useForm<MemoryFormValues>({
    resolver: zodResolver(currentSchema),
    defaultValues: {
      name: '',
      namespace: '',
      providerType: '',
      apiKey: '',
      providerParams: {
        topK: 5,
        scoreThreshold: 0.10,
      },
    },
  })

  useEffect(() => {
    form.reset(form.getValues());
  }, [currentSchema, form.reset, form.getValues]);

  useEffect(() => {
    async function loadProviders() {
      try {
        const response = await getSupportedMemoryProviders()
        if (!response.error && response.data) {
          setProviders(response.data)
          
          // If in edit mode, load the memory details after providers are loaded
          if (editMode && memoryNameToEdit) {
            const memoryRef = k8sRefUtils.toRef(memoryNamespaceToEdit || "", memoryNameToEdit)
            await loadMemoryForEditing(memoryRef, response.data)
          }
        } else {
          throw new Error(response.error || 'Failed to load providers')
        }
      } catch (error) {
        toast.error(`Error loading providers: ${error instanceof Error ? error.message : String(error)}`)
      }
    }
    loadProviders()
  }, [editMode, memoryNameToEdit, memoryNamespaceToEdit])

  const loadMemoryForEditing = async (memoryRef: string, availableProviders: Provider[]) => {
    try {
      setIsLoading(true)
      const memory = await getMemory(memoryRef)
      const memoryRespRef = k8sRefUtils.fromRef(memory.ref)
      // Find the correct provider
      const provider = availableProviders.find(p => p.type === memory.providerName)
      if (provider) {
        setSelectedProvider(provider)
        
        // Set form values
        form.setValue('name', memoryRespRef.name)
        form.setValue('namespace', memoryRespRef.namespace)
        form.setValue('providerType', provider.type)
        // We don't need to set API key in edit mode as the field will be hidden
        form.setValue('apiKey', '')
        
        // Set provider params
        if (memory.memoryParams) {
          Object.entries(memory.memoryParams).forEach(([key, value]) => {
            if (value !== null && value !== undefined) {
              form.setValue(`providerParams.${key}`, value)
            }
          })
        }
      }
    } catch (error) {
      toast.error(`Error loading memory: ${error instanceof Error ? error.message : String(error)}`)
    } finally {
      setIsLoading(false)
    }
  }

  const onSubmit = async (values: MemoryFormValues) => {
    setIsLoading(true)
    toast.info(editMode ? 'Updating memory...' : 'Creating memory...')

    if (!selectedProvider) {
        toast.error('Selected provider not found. Please refresh.')
        setIsLoading(false)
        return
    }

    // Base data for the request
    const memoryData: CreateMemoryRequest = {
      ref: k8sRefUtils.toRef(values.namespace || "", values.name),
      provider: { type: values.providerType },
      apiKey: values.apiKey,
    }

    const params = values.providerParams || {};
    if (selectedProvider.type.toLocaleLowerCase() === 'pinecone') {
      const pineconePayload: PineconeConfigPayload = {
        // Ensure indexHost is a string, default to empty if not provided
        indexHost: typeof params.indexHost === 'string' ? params.indexHost : '',
      };

      // Handle optional numeric fields
      if (params.topK !== undefined && params.topK !== '') {
        const topKNum = Number(params.topK);
        if (!isNaN(topKNum)) {
          pineconePayload.topK = topKNum;
        }
      }

      // Handle optional string fields
      if (typeof params.namespace === 'string' && params.namespace.trim() !== '') {
        pineconePayload.namespace = params.namespace;
      }

      // Handle optional string array field
      if (typeof params.recordFields === 'string' && params.recordFields.trim() !== '') {
        pineconePayload.recordFields = params.recordFields
          .split(',')
          .map((f: string) => f.trim())
          .filter((f: string) => f);
      }

      // Handle optional scoreThreshold (expecting string in payload type)
       if (params.scoreThreshold !== undefined && params.scoreThreshold !== '') {
         const thresholdNum = Number(params.scoreThreshold);
         if (!isNaN(thresholdNum)) {
            // Assuming the type PineconeConfigPayload expects a string here, convert back
           pineconePayload.scoreThreshold = thresholdNum.toString(); 
         }
       }

      memoryData.pinecone = pineconePayload;
    }

    try {
      if (editMode) {
        // Remove the API key and provider params from the memory data
        const { apiKey: _apiKey, provider: _provider, ...rest } = memoryData;
        await updateMemory(rest)
      } else {
        await createMemory(memoryData)
      }
      toast.success(`Memory "${values.name}" ${editMode ? 'updated' : 'created'} successfully!`)
      router.push('/memories')
    } catch (error) {
      toast.error(`Failed to ${editMode ? 'update' : 'create'} memory: ${error instanceof Error ? error.message : String(error)}`)
      setIsLoading(false)
    }
  }

  const handleProviderChange = (value: string) => {
     const provider = providers.find(p => p.type === value)
     setSelectedProvider(provider || null)
     form.setValue('providerType', value)
     form.setValue('providerParams', {});
     form.trigger();
  }

  const getAllParams = () => {
    if (!selectedProvider) return [];
    return [...selectedProvider.requiredParams, ...selectedProvider.optionalParams];
  }

  return (
    <div className="container mx-auto py-10 max-w-2xl">
      <Card>
        <CardHeader>
          <CardTitle>{editMode ? 'Edit Memory' : 'Create New Memory'}</CardTitle>
          <CardDescription>{editMode ? 'Update your memory provider connection.' : 'Configure a new memory provider connection.'}</CardDescription>
        </CardHeader>
        <Form {...form}>
          <form onSubmit={form.handleSubmit(onSubmit)}>
            <CardContent className="space-y-6">
              <FormField
                control={form.control}
                name="name"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>Name</FormLabel>
                    <FormControl>
                      <Input placeholder="e.g., my-pinecone-memory" {...field} disabled={editMode} />
                    </FormControl>
                    <FormDescription>
                      {editMode 
                        ? "Memory name cannot be changed when editing." 
                        : "A unique name for this memory configuration."}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name="namespace"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>Namespace</FormLabel>
                    <FormControl>
                      <NamespaceCombobox
                        value={field.value || ""}
                        onValueChange={field.onChange}
                        disabled={editMode}
                      />
                    </FormControl>
                    <FormDescription>
                      {editMode 
                        ? "Memory namespace cannot be changed when editing."
                        : "A namespace for this memory configuration."}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name="providerType"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>Provider</FormLabel>
                    {editMode ? (
                      <>
                        <FormControl>
                          <Input 
                            value={selectedProvider?.name ? `${selectedProvider.name} (${selectedProvider.type})` : 'Loading...'}
                            disabled={true}
                          />
                        </FormControl>
                        <FormDescription>
                          Provider cannot be changed when editing.
                        </FormDescription>
                      </>
                    ) : (
                      <>
                        <Select onValueChange={handleProviderChange} defaultValue={field.value}>
                          <FormControl>
                            <SelectTrigger>
                              <SelectValue placeholder="Select a memory provider" />
                            </SelectTrigger>
                          </FormControl>
                          <SelectContent>
                            {providers.map((provider) => (
                              <SelectItem key={provider.type} value={provider.type}>
                                {provider.name} ({provider.type})
                              </SelectItem>
                            ))}
                          </SelectContent>
                        </Select>
                        <FormDescription>Choose the type of memory provider.</FormDescription>
                      </>
                    )}
                    <FormMessage />
                  </FormItem>
                )}
              />

              {/* Only show API Key field when not in edit mode */}
              {!editMode && (
                <FormField
                  control={form.control}
                  name="apiKey"
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>API Key</FormLabel>
                      <FormControl>
                        <Input type="password" placeholder="Enter API Key" {...field} />
                      </FormControl>
                      <FormDescription>Your API key for the selected provider.</FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              )}

              {selectedProvider && getAllParams().map((paramName) => {
                const isRequired = selectedProvider.requiredParams.includes(paramName);
                let inputType = "text";
                let inputProps = {};
                
                if (paramName.toLowerCase().includes('key') || paramName.toLowerCase().includes('secret')) {
                  inputType = "password";
                }
                else if (paramName.toLowerCase().includes('topk')) {
                  inputType = "number";
                  inputProps = { min: 1 };
                }
                else if (paramName.toLowerCase().includes('threshold')) {
                  inputType = "number";
                  inputProps = { step: "0.01", min: 0, max: 1 };
                }

                return (
                  <FormField
                    key={paramName}
                    control={form.control}
                    name={`providerParams.${paramName}`}
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>
                          {paramLabels[paramName] || paramName}{isRequired ? ' *' : ' (Optional)'}
                        </FormLabel>
                        <FormControl>
                          <Input
                             type={inputType}
                             placeholder={`Enter ${paramLabels[paramName] || paramName}`}
                             {...field}
                             value={field.value ?? ''}
                             {...inputProps}
                           />
                        </FormControl>
                        <FormDescription>
                          {paramDescriptions[paramName] || `Configuration parameter for ${selectedProvider.name}`}
                        </FormDescription>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                );
              })}
            </CardContent>
            <CardFooter className="flex justify-end">
              <Button type="button" variant="ghost" onClick={() => router.back()} disabled={isLoading}>
                Cancel
              </Button>
              <Button type="submit" disabled={isLoading} className="ml-2">
                {isLoading ? (editMode ? 'Updating...' : 'Creating...') : (editMode ? 'Update Memory' : 'Create Memory')}
              </Button>
            </CardFooter>
          </form>
        </Form>
      </Card>
    </div>
  )
} 