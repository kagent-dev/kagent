"""Built-in tools are independent of skills and respect runtime settings."""

from types import SimpleNamespace

import pytest
from google.adk.agents import LlmAgent
from google.adk.tools.skill_toolset import SkillToolset

from kagent.adk.cli import add_tools_with_config
from kagent.adk.tools import add_builtin_tools_to_agent
from kagent.adk.types import parse_config


@pytest.fixture(autouse=True)
def workspace(tmp_path, monkeypatch):
    monkeypatch.setattr("kagent.tools.workspace.WORKSPACE", tmp_path / "workspace")


@pytest.mark.parametrize("skills_mode", ["missing", "empty", "installed"])
@pytest.mark.parametrize("disable_bash", [False, True])
@pytest.mark.parametrize("file_search", [False, True])
def test_builtin_tools(tmp_path, skills_mode, disable_bash, file_search):
    skills_dir = tmp_path / "skills"
    if skills_mode != "missing":
        skills_dir.mkdir()
    if skills_mode == "installed":
        (skills_dir / "demo").mkdir()
        (skills_dir / "demo" / "SKILL.md").write_text("---\nname: demo\ndescription: Demo skill.\n---\nRead files.\n")
    agent = LlmAgent(name="test_agent", model="gemini-2.0-flash")
    add_builtin_tools_to_agent(
        skills_dir, agent, disable_bash=disable_bash, disable_file_search=not file_search, bash_venv_path="/opt/venv"
    )
    names = {tool.name for tool in agent.tools if not isinstance(tool, SkillToolset)}
    assert {"read_file", "write_file", "edit_file"} <= names
    assert any(isinstance(tool, SkillToolset) for tool in agent.tools) == (skills_mode == "installed")
    assert ("bash" in names) == (not disable_bash)
    assert ("list_files" in names) == file_search
    assert ("grep_file" in names) == file_search
    if not disable_bash:
        assert next(tool for tool in agent.tools if getattr(tool, "name", None) == "bash").bash_venv_path == "/opt/venv"


@pytest.mark.parametrize("disable_bash", [False, True])
def test_runtime_tools_reach_subagents_without_skills(disable_bash):
    child = LlmAgent(name="child", model="gemini-2.0-flash")
    root = LlmAgent(name="root", model="gemini-2.0-flash", sub_agents=[child])
    config = parse_config({"runtime": {"builtin_tools": {"disable_bash": disable_bash}}, "agent": {}})
    add_tools_with_config(root, config)
    for agent in [root, child]:
        names = {tool.name for tool in agent.tools if not isinstance(tool, SkillToolset)}
        assert {"read_file", "write_file", "edit_file"} <= names
        assert ("bash" in names) == (not disable_bash)
        assert {"list_files", "grep_file"} <= names


def test_builtin_tools_with_no_config():
    agent = LlmAgent(name="root", model="gemini-2.0-flash")
    add_tools_with_config(agent)
    assert {tool.name for tool in agent.tools} == {
        "read_file",
        "write_file",
        "edit_file",
        "list_files",
        "grep_file",
        "bash",
    }


@pytest.mark.asyncio
async def test_builtin_file_and_shell_calls_without_skills(tmp_path):
    agent = LlmAgent(name="test_agent", model="gemini-2.0-flash")
    add_builtin_tools_to_agent(tmp_path / "missing-skills", agent)
    tools = {tool.name: tool for tool in agent.tools}
    context = SimpleNamespace(session=SimpleNamespace(id="ignored-actor-local-session-id"))

    async def run(name, args):
        return await tools[name].run_async(args=args, tool_context=context)

    assert "Error" not in await run("write_file", {"file_path": "notes.txt", "content": "hello"})
    assert "hello" in await run("read_file", {"file_path": "notes.txt"})
    assert "Error" not in await run(
        "edit_file", {"file_path": "notes.txt", "old_string": "hello", "new_string": "updated"}
    )
    assert await run("bash", {"command": "cat notes.txt"}) == "updated"
    assert "notes.txt" in await run("list_files", {})
    absolute = str(tmp_path / "absolute.txt")
    assert "Error" not in await run("write_file", {"file_path": absolute, "content": "sandbox"})
    assert "sandbox" in await run("read_file", {"file_path": absolute})
