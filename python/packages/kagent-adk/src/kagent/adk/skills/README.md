# ADK Skills

Filesystem-based skills with progressive disclosure and two-tool architecture for domain expertise.

---

## Quick Start

### Recommended: Plugin-Based (Multi-Agent Apps)

```python
from kagent.adk.skills import SkillsPlugin

# Plugin automatically initializes sessions and registers all skills tools
app = App(
    root_agent=agent,
    plugins=[SkillsPlugin(skills_directory="./skills")]
)
```

**Benefits:**

- ✅ Session paths initialized before any tool runs
- ✅ Automatic tool registration on all agents
- ✅ Handles custom skills directories correctly
- ✅ No tool call order dependencies

### Alternative: Direct Tool Usage

```python
from kagent.adk.skills import SkillsTool
from kagent.adk.tools import BashTool, ReadFileTool, WriteFileTool, EditFileTool

agent = Agent(
    tools=[
        SkillsTool(skills_directory="./skills"),
        BashTool(skills_directory="./skills"),
        ReadFileTool(),
        WriteFileTool(),
        EditFileTool(),
    ]
)
```

**Note:** Without SkillsPlugin, sessions auto-initialize with `/skills` directory. For custom skills paths, use the plugin.

---

## Session Initialization

Skills uses a **plugin-based initialization pattern** to ensure session working directories are set up before any tools run.

### How It Works

```text
App Starts
    ↓
SkillsPlugin initialized with skills_directory
    ↓
First Agent Turn
    ↓
before_agent_callback() hook fires
    ↓
Session path initialized with skills symlink
    ↓
Tools registered on agent
    ↓
Tools execute (session already initialized)
```

**Key Points:**

- `SkillsPlugin.before_agent_callback()` fires **before any tool invocation**
- Creates `/tmp/kagent/{session_id}/` with `skills/` symlink
- All tools use `get_session_path(session_id)` which returns cached path
- **No tool call order dependencies** - session always ready

**Without Plugin:**

- Tools auto-initialize session with default `/skills` on first call
- Works fine if skills are at `/skills` location
- For custom paths, use SkillsPlugin

---

## Architecture

### Skill Structure

```text
skills/
├── data-analysis/
│   ├── SKILL.md        # Metadata (YAML frontmatter) + instructions
│   └── scripts/        # Python scripts, configs, etc.
│       └── analyze.py
```

**SKILL.md Example:**

```markdown
---
name: data-analysis
description: Analyze CSV/Excel files
---

# Data Analysis

...instructions for the agent...
```

### Tool Workflow

**Three Phases:**

1. **Discovery** - Agent sees available skills in SkillsTool description
2. **Loading** - Agent calls `skills(command='data-analysis')` → gets full SKILL.md
3. **Execution** - Agent uses BashTool + file tools to run scripts per instructions

| Tool           | Purpose                      | Example                                               |
| -------------- | ---------------------------- | ----------------------------------------------------- |
| **SkillsTool** | Load skill instructions      | `skills(command='data-analysis')`                     |
| **BashTool**   | Execute commands             | `bash("cd skills/data-analysis && python script.py")` |
| **ReadFile**   | Read files with line numbers | `read_file("skills/data-analysis/config.json")`       |
| **WriteFile**  | Create/overwrite files       | `write_file("outputs/report.pdf", data)`              |
| **EditFile**   | Precise string replacements  | `edit_file("script.py", old="x", new="y")`            |

### Working Directory Structure

Each session gets an isolated working directory with symlinked skills:

```text
/tmp/kagent/{session_id}/
├── skills/      → symlink to /skills (read-only, shared across sessions)
├── uploads/     → staged user files (writable)
├── outputs/     → generated files for download (writable)
└── *.py         → temporary scripts (writable)
```

**Path Resolution:**

- Relative paths resolve from working directory: `skills/data-analysis/script.py`
- Absolute paths work too: `/tmp/kagent/{session_id}/outputs/report.pdf`
- Skills symlink enables natural relative references while maintaining security

---

## Artifact Handling

User uploads and downloads are managed through artifact tools:

```python
# 1. Stage uploaded file from artifact service
stage_artifacts(artifact_names=["sales_data.csv"])
# → Writes to: uploads/sales_data.csv

# 2. Agent processes file
bash("python skills/data-analysis/scripts/analyze.py uploads/sales_data.csv")
# → Script writes: outputs/report.pdf

# 3. Return generated file
return_artifacts(file_paths=["outputs/report.pdf"])
# → Saves to artifact service for user download
```

**Flow:** User Upload → Artifact Service → `uploads/` → Processing → `outputs/` → Artifact Service → User Download

---

## Security

**Read-only skills directory:**

- Skills at `/skills` are read-only (enforced by sandbox)
- Symlink at `skills/` inherits read-only permissions
- Agents cannot modify skill code or instructions

**File tools:**

- Path traversal protection (no `..`)
- Session isolation (each session has separate working directory)
- File size limits (100 MB max)

**Bash tool:**

- Sandboxed execution via Anthropic Sandbox Runtime
- Command timeouts (30s default, 120s for pip install)
- Working directory restrictions

---

## Example Agent Flow

```python
# User asks: "Analyze my sales data"

# 1. Agent discovers available skills
#    → SkillsTool description lists: data-analysis, pdf-processing, etc.

# 2. Agent loads skill instructions
agent: skills(command='data-analysis')
#    → Returns full SKILL.md with detailed instructions

# 3. Agent stages uploaded file
agent: stage_artifacts(artifact_names=["sales_data.csv"])
#    → File available at: uploads/sales_data.csv

# 4. Agent reads skill script to understand it
agent: read_file("skills/data-analysis/scripts/analyze.py")

# 5. Agent executes analysis
agent: bash("cd skills/data-analysis && python scripts/analyze.py ../../uploads/sales_data.csv")
#    → Script generates: outputs/analysis_report.pdf

# 6. Agent returns result
agent: return_artifacts(file_paths=["outputs/analysis_report.pdf"])
#    → User can download report.pdf
```
