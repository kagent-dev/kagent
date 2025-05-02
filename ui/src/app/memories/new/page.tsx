'use client'

import React, { useState, useEffect } from 'react'
import { useRouter } from 'next/navigation'
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
import { createMemory } from '@/app/actions/memories'
import { Provider, CreateMemoryRequest, PineconeConfigPayload } from '@/lib/types'

// Base schema
const baseFormSchema = z.object({
  name: z.string().min(1, "Name is required"),
  providerType: z.string().min(1, "Provider is required"),
  apiKey: z.string().min(1, "API Key is required"),
  // Generic object to hold dynamic provider parameters
  providerParams: z.record(z.string(), z.any()).optional(),
})

// Function to create a refined schema based on selected provider
const createRefinedSchema = (selectedProvider: Provider | null) => {
  return baseFormSchema.refine((data) => {
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
  const [providers, setProviders] = useState<Provider[]>([])
  const [isLoading, setIsLoading] = useState(false)
  const [selectedProvider, setSelectedProvider] = useState<Provider | null>(null)
  const [currentSchema, setCurrentSchema] = useState(createRefinedSchema(null));

  useEffect(() => {
    setCurrentSchema(createRefinedSchema(selectedProvider));
  }, [selectedProvider]);

  type MemoryFormValues = z.infer<typeof baseFormSchema>;

  const form = useForm<MemoryFormValues>({
    resolver: zodResolver(currentSchema),
    defaultValues: {
      name: '',
      providerType: '',
      apiKey: '',
      providerParams: {},
    },
  })

  useEffect(() => {
    form.reset(form.getValues());
  }, [currentSchema, form.reset, form.getValues]);

  useEffect(() => {
    async function loadProviders() {
      try {
        const response = await getSupportedMemoryProviders()
        if (response.success && response.data) {
          setProviders(response.data)
        } else {
          throw new Error(response.error || 'Failed to load providers')
        }
      } catch (error) {
        toast.error(`Error loading providers: ${error instanceof Error ? error.message : String(error)}`)
      }
    }
    loadProviders()
  }, [])

  const onSubmit = async (values: MemoryFormValues) => {
    setIsLoading(true)
    toast.info('Creating memory...')

    if (!selectedProvider) {
        toast.error('Selected provider not found. Please refresh.')
        setIsLoading(false)
        return
    }

    // Base data for the request
    const memoryData: CreateMemoryRequest = {
      name: values.name,
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
      await createMemory(memoryData)
      toast.success(`Memory "${values.name}" created successfully!`)
      router.push('/memories')
    } catch (error) {
      toast.error(`Failed to create memory: ${error instanceof Error ? error.message : String(error)}`)
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
          <CardTitle>Create New Memory</CardTitle>
          <CardDescription>Configure a new memory provider connection.</CardDescription>
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
                      <Input placeholder="e.g., my-pinecone-memory" {...field} />
                    </FormControl>
                    <FormDescription>A unique name for this memory configuration.</FormDescription>
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
                    <FormMessage />
                  </FormItem>
                )}
              />

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

              {selectedProvider && getAllParams().map((paramName) => {
                const isRequired = selectedProvider.requiredParams.includes(paramName);
                let inputType = "text";
                if (paramName.toLowerCase().includes('key') || paramName.toLowerCase().includes('secret')) {
                  inputType = "password";
                }
                if (paramName.toLowerCase().includes('number') || paramName.toLowerCase().includes('topk') || paramName.toLowerCase().includes('threshold')) {
                    inputType = "number";
                }

                return (
                  <FormField
                    key={paramName}
                    control={form.control}
                    name={`providerParams.${paramName}`}
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{paramName}{isRequired ? ' *' : ' (Optional)'}</FormLabel>
                        <FormControl>
                          <Input
                             type={inputType}
                             placeholder={`Enter ${paramName}`}
                             {...field}
                             value={field.value ?? ''}
                           />
                        </FormControl>
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
                {isLoading ? 'Creating...' : 'Create Memory'}
              </Button>
            </CardFooter>
          </form>
        </Form>
      </Card>
    </div>
  )
} 