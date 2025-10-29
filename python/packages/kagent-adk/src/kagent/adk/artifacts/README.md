# Simplified Artifact Handling Design

## Overview

This design keeps artifact handling **simple, safe, and robust** by following these principles:

1. **Artifact names ARE filenames** - No separate metadata files needed
2. **MIME types for extensions** - Automatically add proper extensions when needed
3. **Clear directory structure** - Predictable working environment
4. **Minimal path manipulation** - Simple, straightforward staging

---

## Architecture

### Working Directory Structure

```
/tmp/adk_sessions/{app_name}/{session_id}/
├── skills/      → symlink to static skills (read-only)
├── uploads/     → staged user files (temporary)
└── outputs/     → generated files for return
```

### Data Flow

```
UPLOAD FLOW:
User → Frontend → Artifact Service (saves with filename)
     → stage_artifacts tool → Load from context
     → Write to uploads/filename.ext
     → bash tool uses uploads/filename.ext

DOWNLOAD FLOW:
bash tool → Generates outputs/report.pdf
         → return_artifacts tool → Read from outputs/
         → Save to context.save_artifact("report.pdf")
         → User downloads via frontend
```

## Tool API

### StageArtifactsTool

**Purpose:** Stage uploaded files from artifact service to working directory

**Input:**

```python
stage_artifacts(
    artifact_names=["data.csv"],      # Artifact names (should be filenames)
    destination_path="uploads/"        # Optional, defaults to "uploads/"
)
```

**Output:**

```
Successfully staged 1 file(s):
  • uploads/data.csv (1.2 MB)
```

**What it does:**

1. Load artifact from `context.load_artifact("data.csv")`
2. Check file size (< 100 MB)
3. Ensure proper extension based on MIME type
4. Write to `working_dir/uploads/data.csv`
5. Return paths to agent

### ReturnArtifactsTool

**Purpose:** Save generated files back to artifact service for user download

**Input:**

```python
return_artifacts(
    file_paths=["outputs/report.pdf"],  # Relative paths from working_dir
    artifact_names=["report.pdf"]       # Optional custom names
)
```

**Output:**

```
Saved 1 file(s) for download:
  • report.pdf (v0, 15.2 KB)
```

**What it does:**

1. Read file from `working_dir/outputs/report.pdf`
2. Check file size (< 100 MB)
3. Detect MIME type from extension
4. Save to `context.save_artifact("report.pdf", artifact_part)`
5. Return confirmation to agent

---

## Complete Workflow Example

```python
# 1. USER UPLOADS FILE
# Frontend saves to artifact service as "sales_data.csv"

# 2. AGENT STAGES FILE
stage_artifacts(artifact_names=["sales_data.csv"])
# → Stages to: uploads/sales_data.csv

# 3. AGENT PROCESSES FILE
bash("cd skills/data-analysis && python scripts/analyze.py ../../uploads/sales_data.csv")
# → Script reads: working_dir/uploads/sales_data.csv
# → Script writes: working_dir/outputs/analysis_report.pdf

# 4. AGENT RETURNS OUTPUT
return_artifacts(file_paths=["outputs/analysis_report.pdf"])
# → Saves to artifact service as "analysis_report.pdf"

# 5. USER DOWNLOADS
# Frontend fetches artifact "analysis_report.pdf" and downloads
```

## Security & Safety

### Built-in Protection:

1. **File Size Limits:** 100 MB max for both upload and download
2. **Path Traversal:** All paths validated to stay within working directory
3. **Session Isolation:** Each session has isolated working directory

### What's NOT Protected (by design):

- **Cleanup:** Session directories persist in `/tmp/adk_sessions/`
  - _Future:_ Add session lifecycle cleanup
  - _Workaround:_ Periodic cleanup job or TTL
