"""Tests for add_skills_tool_to_agent's list_files/grep_file feature-flag gating."""

from google.adk.agents import LlmAgent

from kagent.adk.tools.skills_plugin import add_skills_tool_to_agent


def _tool_names(agent: LlmAgent) -> set[str]:
    return {getattr(t, "name", None) for t in agent.tools}


def test_add_skills_tool_to_agent_omits_list_files_and_grep_file_by_default(tmp_path):
    agent = LlmAgent(name="test_agent", model="gemini-2.0-flash", tools=[])

    add_skills_tool_to_agent(str(tmp_path), agent)

    names = _tool_names(agent)
    assert {"skills", "read_file", "write_file", "edit_file", "bash"} <= names
    assert "list_files" not in names
    assert "grep_file" not in names


def test_add_skills_tool_to_agent_adds_list_files_and_grep_file_when_enabled(tmp_path):
    agent = LlmAgent(name="test_agent", model="gemini-2.0-flash", tools=[])

    add_skills_tool_to_agent(str(tmp_path), agent, file_search_tools=True)

    names = _tool_names(agent)
    assert {"skills", "read_file", "write_file", "edit_file", "bash", "list_files", "grep_file"} <= names
