## Problem

The original issue was that when Kubernetes objects are deleted, the reconciler receives deletion events but the objects themselves are no longer available in the API. This caused `utils.GetObjectRef()` to return empty strings when called on nil/empty objects, breaking database cleanup logic.

However, during the fix, we discovered a deeper database schema issue: the `DeleteToolsForServer` method was trying to filter by `group_kind` column on the Tool table, but Tools were missing the GroupKind field entirely, leading to SQL errors.

## Solution

This PR provides a comprehensive fix addressing both the original bug and the underlying database schema issues:

### 1. **Nil Object Reference Fix**
Fixed the nil object reference bug in three reconcile functions by using `req.NamespacedName.String()` instead of `utils.GetObjectRef()` when handling `IsNotFound` errors:

- **`ReconcileKagentMCPServer`**: Fixed deletion logic for MCP servers
- **`ReconcileKagentRemoteMCPServer`**: Fixed deletion logic for remote MCP servers  
- **`ReconcileKagentMCPService`**: Fixed deletion logic for MCP services

### 2. **Database Schema Enhancement (BREAKING CHANGE)**
Implemented proper GroupKind support throughout the Tool database operations:

- **Added GroupKind field** to Tool model as primary key
- **Updated all database operations** to use both serverName and groupKind
- **Enhanced tool isolation** between different server types
- **Fixed SQL logic errors** by ensuring proper schema consistency

### 3. **Complete GroupKind Integration**
- **DeleteToolsForServer**: Now filters by both `server_name` AND `group_kind`
- **ListToolsForServer**: Updated to require `groupKind` parameter
- **RefreshToolsForServer**: Enhanced to handle `groupKind` when creating/updating tools
- **All reconciler calls**: Updated to pass `toolServer.GroupKind` parameter
- **HTTP handlers**: Updated to use groupKind when listing tools
- **Service support**: Added complete Service GroupKind case to toolserver deletion handler

## Changes

### Database Model Changes
- Added `GroupKind` field to `Tool` model as `gorm:"primaryKey;not null"`
- Tool table now has composite primary key: `(id, server_name, group_kind)`

### Client Interface Updates
- `ListToolsForServer(serverName string, groupKind string) ([]Tool, error)`
- `RefreshToolsForServer(serverName string, groupKind string, tools ...*v1alpha2.MCPTool) error`
- `DeleteToolsForServer(serverName string, groupKind string) error` - now properly filters by both parameters

### Reconciler Fixes
- All `IsNotFound` error handling now uses `req.NamespacedName.String()` for proper object identification
- All database operations now pass the correct `groupKind` parameter

### HTTP Handler Updates
- Toolserver deletion now supports all three GroupKind types:
  - `"Service"` - for Kubernetes Services
  - `"MCPServer.kagent.dev"` - for MCP Server CRDs
  - `"RemoteMCPServer.kagent.dev"` - for Remote MCP Server CRDs

## Breaking Change

⚠️ **BREAKING CHANGE**: This PR includes breaking database schema changes.

**Migration Required**: Users must delete their existing database and allow GORM to recreate the schema. GORM AutoMigrate cannot safely add NOT NULL primary key fields to existing data.

The Tool table primary key has changed from `(id, server_name)` to `(id, server_name, group_kind)`.

## Issues Resolved

- closes #770
