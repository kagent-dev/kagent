package client

import (
	"context"
	"fmt"

	"github.com/kagent-dev/kagent/go/controller/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/pkg/client/api"
)

// Memory defines the memory operations
type Memory interface {
	ListMemories(ctx context.Context) (*api.StandardResponse[[]api.MemoryResponse], error)
	CreateMemory(ctx context.Context, request *api.CreateMemoryRequest) (*api.StandardResponse[*v1alpha1.Memory], error)
	GetMemory(ctx context.Context, namespace, memoryName string) (*api.StandardResponse[*api.MemoryResponse], error)
	UpdateMemory(ctx context.Context, namespace, memoryName string, request *api.UpdateMemoryRequest) (*api.StandardResponse[*v1alpha1.Memory], error)
	DeleteMemory(ctx context.Context, namespace, memoryName string) error
}

// memoryClient handles memory-related requests
type memoryClient struct {
	client *BaseClient
}

// NewMemoryClient creates a new memory client
func NewMemoryClient(client *BaseClient) Memory {
	return &memoryClient{client: client}
}

// ListMemories lists all memories
func (c *memoryClient) ListMemories(ctx context.Context) (*api.StandardResponse[[]api.MemoryResponse], error) {
	resp, err := c.client.Get(ctx, "/api/memories", "")
	if err != nil {
		return nil, err
	}

	var memories api.StandardResponse[[]api.MemoryResponse]
	if err := DecodeResponse(resp, &memories); err != nil {
		return nil, err
	}

	return &memories, nil
}

// CreateMemory creates a new memory
func (c *memoryClient) CreateMemory(ctx context.Context, request *api.CreateMemoryRequest) (*api.StandardResponse[*v1alpha1.Memory], error) {
	resp, err := c.client.Post(ctx, "/api/memories", request, "")
	if err != nil {
		return nil, err
	}

	var memory api.StandardResponse[*v1alpha1.Memory]
	if err := DecodeResponse(resp, &memory); err != nil {
		return nil, err
	}

	return &memory, nil
}

// GetMemory retrieves a specific memory
func (c *memoryClient) GetMemory(ctx context.Context, namespace, memoryName string) (*api.StandardResponse[*api.MemoryResponse], error) {
	path := fmt.Sprintf("/api/memories/%s/%s", namespace, memoryName)
	resp, err := c.client.Get(ctx, path, "")
	if err != nil {
		return nil, err
	}

	var memory api.StandardResponse[*api.MemoryResponse]
	if err := DecodeResponse(resp, &memory); err != nil {
		return nil, err
	}

	return &memory, nil
}

// UpdateMemory updates an existing memory
func (c *memoryClient) UpdateMemory(ctx context.Context, namespace, memoryName string, request *api.UpdateMemoryRequest) (*api.StandardResponse[*v1alpha1.Memory], error) {
	path := fmt.Sprintf("/api/memories/%s/%s", namespace, memoryName)
	resp, err := c.client.Put(ctx, path, request, "")
	if err != nil {
		return nil, err
	}

	var memory api.StandardResponse[*v1alpha1.Memory]
	if err := DecodeResponse(resp, &memory); err != nil {
		return nil, err
	}

	return &memory, nil
}

// DeleteMemory deletes a memory
func (c *memoryClient) DeleteMemory(ctx context.Context, namespace, memoryName string) error {
	path := fmt.Sprintf("/api/memories/%s/%s", namespace, memoryName)
	resp, err := c.client.Delete(ctx, path, "")
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}
