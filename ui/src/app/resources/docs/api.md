# API Reference

## Base URL
```
https://api.adolphe.ai/v1
```

## Authentication
All API requests require an API key in the `Authorization` header:
```
Authorization: Bearer YOUR_API_KEY
```

## Endpoints

### 1. Chat Completion
Create a chat completion with the specified agent.

**Endpoint**: `POST /chat/completions`

**Request Body**:
```json
{
  "agent_id": "customer-support",
  "messages": [
    {"role": "user", "content": "Hello!"}
  ],
  "stream": false
}
```

**Response**:
```json
{
  "id": "chatcmpl-123",
  "object": "chat.completion",
  "created": 1677652288,
  "model": "gpt-4",
  "choices": [{
    "index": 0,
    "message": {
      "role": "assistant",
      "content": "Hello! How can I help you today?"
    },
    "finish_reason": "stop"
  }],
  "usage": {
    "prompt_tokens": 9,
    "completion_tokens": 9,
    "total_tokens": 18
  }
}
```

### 2. List Agents
Get a list of available agents.

**Endpoint**: `GET /agents`

**Query Parameters**:
- `limit`: Number of agents to return (default: 20)
- `offset`: Number of agents to skip (default: 0)

**Response**:
```json
{
  "data": [
    {
      "id": "customer-support",
      "name": "Customer Support",
      "description": "Handles customer inquiries",
      "created_at": "2023-01-01T00:00:00Z"
    }
  ],
  "pagination": {
    "total": 1,
    "limit": 20,
    "offset": 0
  }
}
```

## Error Handling

### Error Response Format
```json
{
  "error": {
    "code": "invalid_request_error",
    "message": "Invalid API key",
    "param": "authorization",
    "type": "authentication_error"
  }
}
```

### Common Error Codes
- `400`: Bad Request - Invalid request format
- `401`: Unauthorized - Invalid or missing API key
- `403`: Forbidden - Insufficient permissions
- `404`: Not Found - Resource not found
- `429`: Too Many Requests - Rate limit exceeded
- `500`: Internal Server Error - Server error

## Rate Limiting
- Free tier: 100 requests/minute
- Pro tier: 1,000 requests/minute
- Enterprise: Custom limits available

## Webhooks
Set up webhooks to receive real-time events:
- `message.received`: New message received
- `message.sent`: Message sent to user
- `agent.error`: Error occurred during processing

Example webhook payload:
```json
{
  "event": "message.received",
  "data": {
    "message_id": "msg_123",
    "agent_id": "customer-support",
    "content": "Hello!",
    "timestamp": "2023-01-01T00:00:00Z"
  }
}
```
