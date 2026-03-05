# Rough Idea

Create MCP Server using Official GO MCP SDK for Kanban board API

## Source

1. Create MCP server which allows to manage board with tasks
2. Design persistent layer using GORM with SQLLite and Postgres support
3. Each Task should have status Inbox, Design, Develop,Testing, SecurityScan,CodeReview,Documentation,Done 
4. Every Task should have User input needed flag for Human in the loop 
5. Same MCP Server should have simple UI, single page html + js able to show data realtime
