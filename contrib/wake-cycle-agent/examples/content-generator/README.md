# Content Generator Example

A wake-cycle agent that generates and posts content on a schedule.

## Overview

This example demonstrates a practical application of the wake-cycle pattern:
- Wakes every 4 hours
- Reads a content queue from state
- Generates content using the LLM
- Posts to configured destinations
- Tracks performance metrics

## How It Works

### Wake Cycle Flow

```
┌──────────────┐
│  Wake (4h)   │
└──────┬───────┘
       │
       ▼
┌──────────────────┐
│ Read state.json  │
│ - content_queue  │
│ - posted_items   │
│ - metrics        │
└────────┬─────────┘
         │
         ▼
┌──────────────────┐    No items?    ┌──────────────────┐
│ Check queue for  │───────────────▶│ Generate new     │
│ pending items    │                 │ content ideas    │
└────────┬─────────┘                 └────────┬─────────┘
         │                                     │
         │ Has items                          │
         ▼                                     ▼
┌──────────────────┐                 ┌──────────────────┐
│ Pick next item   │◀────────────────│ Add to queue     │
│ by priority      │                 └──────────────────┘
└────────┬─────────┘
         │
         ▼
┌──────────────────┐
│ Generate content │
│ (blog, social)   │
└────────┬─────────┘
         │
         ▼
┌──────────────────┐
│ Post to platform │
│ (via MCP tools)  │
└────────┬─────────┘
         │
         ▼
┌──────────────────┐
│ Update state     │
│ - move to posted │
│ - log metrics    │
└──────────────────┘
```

### State Structure

```json
{
  "content_queue": [
    {
      "id": "content-001",
      "type": "blog_post",
      "topic": "Kubernetes Best Practices",
      "priority": "high",
      "status": "pending",
      "created_at": "2026-02-17T08:00:00Z"
    }
  ],
  "posted_items": [
    {
      "id": "content-000",
      "type": "social",
      "platform": "twitter",
      "posted_at": "2026-02-17T04:00:00Z",
      "url": "https://twitter.com/...",
      "metrics": {
        "impressions": 1234,
        "engagements": 56
      }
    }
  ],
  "metrics": {
    "total_posts": 42,
    "posts_this_week": 7,
    "avg_engagement_rate": 4.2
  }
}
```

## Deployment

1. **Prerequisites**: kagent installed, API keys configured

2. **Apply the manifests**:
   ```bash
   kubectl apply -k .
   ```

3. **Check the agent**:
   ```bash
   kubectl get agents -n content-generator
   kubectl logs -f -n content-generator -l app=content-generator
   ```

4. **View posted content**:
   ```bash
   kubectl exec -it deploy/kagent-engine -n kagent -- \
     cat /data/posted_items.json
   ```

## Configuration

### Environment Variables

| Variable | Description | Required |
|----------|-------------|----------|
| `TWITTER_API_KEY` | Twitter API credentials | For Twitter posting |
| `OPENAI_API_KEY` | OpenAI API key | For content generation |
| `CONTENT_SCHEDULE` | Cron schedule | Default: `0 */4 * * *` |

### Content Types

The agent supports multiple content types:

- **blog_post**: Long-form articles
- **social**: Short posts for Twitter/LinkedIn
- **thread**: Multi-part Twitter threads
- **newsletter**: Email newsletter content

Configure supported types in `constitution.yaml`.

## Customization

### Adding a New Platform

1. Create an MCP ToolServer for the platform API
2. Add the platform to the agent's tools
3. Update the constitution with platform-specific rules
4. Add posting logic to the agent's system message

### Changing the Schedule

Edit `cronjob.yaml`:
```yaml
schedule: "0 */4 * * *"  # Every 4 hours
# schedule: "0 9,14,18 * * *"  # 9 AM, 2 PM, 6 PM
# schedule: "0 * * * *"  # Every hour
```

## Best Practices

1. **Quality over quantity**: Generate fewer, higher-quality posts
2. **Platform-appropriate**: Adapt content for each platform
3. **Engagement tracking**: Monitor metrics and adjust strategy
4. **Rate limiting**: Respect platform API limits
5. **Content review**: Consider human-in-the-loop for sensitive content
