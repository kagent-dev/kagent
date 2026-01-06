from .models import Skill


def generate_skills_xml(skills: list[Skill]) -> str:
    """Formats a list of skills into an XML block for tool descriptions."""
    if not skills:
        return "<available_skills>\n<!-- No skills found -->\n</available_skills>"

    skills_entries = []
    for skill in skills:
        skill_xml = f"<skill>\n<name>{skill.name}</name>\n<description>{skill.description}</description>\n</skill>"
        skills_entries.append(skill_xml)

    return "<available_skills>\n" + "\n".join(skills_entries) + "\n</available_skills>"


def generate_skills_tool_description(skills: list[Skill]) -> str:
    """Generates the full, standardized description for the 'skills' tool."""
    skills_xml = generate_skills_xml(skills)

    # This description is based on the ADK version, which is the source of truth.
    description = f"""Execute a skill within the main conversation

<skills_instructions>
When users ask you to perform tasks, check if any of the available skills below can help complete the task more effectively. Skills provide specialized capabilities and domain knowledge.

How to use skills:
- Invoke skills using this tool with the skill name only (no arguments)
- When you invoke a skill, the skill's full SKILL.md will load with detailed instructions
- Follow the skill's instructions and use the bash tool to execute commands
- Examples:
  - command: \"data-analysis\" - invoke the data-analysis skill
  - command: \"pdf-processing\" - invoke the pdf-processing skill

Important:
- Only use skills listed in <available_skills> below
- Do not invoke a skill that is already loaded in the conversation
- After loading a skill, use the bash tool for execution
- If not specified, scripts are located in the skill-name/scripts subdirectory
</skills_instructions>

{skills_xml}
"""
    return description


def get_read_file_description() -> str:
    """Returns the standardized description for the read_file tool."""
    return """Reads a file from the filesystem with line numbers.

Usage:
- Provide a path to the file (absolute or relative to your working directory)
- Returns content with line numbers (format: LINE_NUMBER|CONTENT)
- Optional offset and limit parameters for reading specific line ranges
- Lines longer than 2000 characters are truncated
- Always read a file before editing it
- You can read from skills/ directory, uploads/, outputs/, or any file in your session
"""


def get_write_file_description() -> str:
    """Returns the standardized description for the write_file tool."""
    return """Writes content to a file on the filesystem.

Usage:
- Provide a path (absolute or relative to working directory) and content to write
- Overwrites existing files
- Creates parent directories if needed
- For existing files, read them first using read_file
- Prefer editing existing files over writing new ones
- You can write to your working directory, outputs/, or any writable location
- Note: skills/ directory is read-only
"""


def get_edit_file_description() -> str:
    """Returns the standardized description for the edit_file tool."""
    return """Performs exact string replacements in files.

Usage:
- You must read the file first using read_file
- Provide path (absolute or relative to working directory)
- When editing, preserve exact indentation from the file content
- Do NOT include line number prefixes in old_string or new_string
- old_string must be unique unless replace_all=true
- Use replace_all to rename variables/strings throughout the file
- old_string and new_string must be different
- Note: skills/ directory is read-only
"""


def get_bash_description() -> str:
    """Returns the standardized description for the bash tool."""
    # This combines the useful parts from both ADK and OpenAI descriptions
    return """Execute bash commands in the skills environment with sandbox protection.

Working Directory & Structure:
- Commands run in a temporary session directory: /tmp/kagent/{session_id}/
- /skills -> All skills are available here (read-only).
- Your current working directory and /skills are added to PYTHONPATH.

Python Imports (CRITICAL):
- To import from a skill, use the name of the skill.
  Example: from skills_name.module import function
- If the skills name contains a dash '-', you need to use importlib to import it.
  Example:
    import importlib
    skill_module = importlib.import_module('skill-name.module')

For file operations:
- Use read_file, write_file, and edit_file for interacting with the filesystem.

Timeouts:
- python scripts: 60s
- other commands: 30s
"""
