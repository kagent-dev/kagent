"""Built-in tools are independent of skills and respect runtime settings."""

import shlex
from types import SimpleNamespace

import pytest
from google.adk.agents import LlmAgent
from google.adk.agents.invocation_context import InvocationContext
from google.adk.flows.llm_flows.tools._agent_tools import process_agent_tools
from google.adk.models.llm_request import LlmRequest
from google.adk.sessions import InMemorySessionService, Session
from google.adk.tools import FunctionTool, ToolContext
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


async def _model_request(agent):
    context = InvocationContext(
        agent=agent,
        invocation_id="test-invocation",
        session=Session(id="test-session", app_name="test", user_id="test-user"),
        session_service=InMemorySessionService(),
    )
    request = LlmRequest(model="gemini-2.0-flash")
    # Exercise ADK's request builder, including declarations and dispatch mapping.
    await process_agent_tools(context, request)
    return request, ToolContext(context)


@pytest.mark.asyncio
@pytest.mark.parametrize("tool_name", ["read_file", "write_file", "edit_file", "list_files", "grep_file", "bash"])
@pytest.mark.parametrize("wrapped", [False, True])
async def test_defaults_preserve_custom_tool_declarations_and_dispatch(tool_name, wrapped):
    def custom_tool(value: str) -> str:
        """Return a value using the user's implementation."""
        return f"custom: {value}"

    custom_tool.__name__ = tool_name
    supplied_tool = FunctionTool(custom_tool) if wrapped else custom_tool
    agent = LlmAgent(name="custom", model="gemini-2.0-flash", tools=[supplied_tool])
    add_tools_with_config(agent)
    add_tools_with_config(agent)
    request, context = await _model_request(agent)

    declarations = [declaration for tool in request.config.tools for declaration in tool.function_declarations or []]
    assert len(declarations) == 6
    assert len({declaration.name for declaration in declarations}) == 6
    assert request.tools_dict[tool_name].func is custom_tool
    assert await request.tools_dict[tool_name].run_async(args={"value": "kept"}, tool_context=context) == "custom: kept"


@pytest.mark.asyncio
@pytest.mark.parametrize("disable_bash", [False, True])
async def test_skill_model_request_matches_bash_availability(tmp_path, disable_bash):
    skill = tmp_path / "skills" / "demo"
    (skill / "scripts").mkdir(parents=True)
    (skill / "SKILL.md").write_text("---\nname: demo\ndescription: Demo skill.\n---\nRun scripts/demo.sh.\n")
    script = skill / "scripts" / "demo.sh"
    script.write_text("printf skill-executed")
    agent = LlmAgent(name="skilled", model="gemini-2.0-flash")
    add_builtin_tools_to_agent(skill.parent, agent, disable_bash=disable_bash)
    add_builtin_tools_to_agent(skill.parent, agent, disable_bash=disable_bash)
    request, context = await _model_request(agent)

    prompt = request.config.system_instruction
    assert "list_skills" in prompt
    assert "load_skill" in prompt
    assert "load_skill_resource" in prompt
    assert "run_skill_script" not in request.tools_dict
    assert ("bash" in request.tools_dict) == (not disable_bash)
    assert sum(isinstance(tool, SkillToolset) for tool in agent.tools) == 1
    loaded = await request.tools_dict["load_skill"].run_async(args={"skill_name": "demo"}, tool_context=context)
    assert loaded["instructions"] == "Run scripts/demo.sh."
    resource = await request.tools_dict["load_skill_resource"].run_async(
        args={"skill_name": "demo", "file_path": "scripts/demo.sh"}, tool_context=context
    )
    assert resource["content"] == "printf skill-executed"
    if disable_bash:
        assert "The bash tool is unavailable" in prompt
        assert "Do not execute bundled scripts" in prompt
        assert "Run bundled scripts with the bash tool" not in prompt
    else:
        assert "Run bundled scripts with the bash tool" in prompt
        assert str(skill.parent.resolve()) in prompt
        assert "cannot run" not in prompt
        assert "Do not execute" not in prompt
        assert "run_skill_script" not in prompt
        assert (
            await request.tools_dict["bash"].run_async(
                args={"command": f"bash {shlex.quote(str(script))}"}, tool_context=context
            )
            == "skill-executed"
        )
