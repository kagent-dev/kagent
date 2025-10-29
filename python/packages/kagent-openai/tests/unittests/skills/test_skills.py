"""Tests for the Skills functionality."""

import tempfile
from pathlib import Path

import pytest
from agents import Agent

from kagent.openai.agents.skills import SkillLoader, SkillRegistry, get_skill_tool


@pytest.fixture
def temp_skill_dir():
    """Create a temporary directory for test skills."""
    with tempfile.TemporaryDirectory() as tmpdir:
        yield Path(tmpdir)


@pytest.fixture
def sample_skill_content():
    """Sample SKILL.md content."""
    return """---
name: test-skill
description: A test skill for unit testing
license: MIT
allowed-tools:
  - bash
  - python
metadata:
  author: Test Author
  version: 1.0.0
---

# Test Skill

This is a test skill with instructions.

## Usage

Follow these test instructions.
"""


@pytest.fixture
def minimal_skill_content():
    """Minimal SKILL.md content with only required fields."""
    return """---
name: minimal-skill
description: Minimal test skill
---

# Minimal Skill

Just the basics.
"""


@pytest.fixture
def registry():
    """Create a fresh skill registry for each test."""
    return SkillRegistry()


class TestSkillLoader:
    """Tests for SkillLoader."""

    def test_parse_skill_md_full(self, temp_skill_dir, sample_skill_content):
        """Test parsing a complete SKILL.md file."""
        skill_dir = temp_skill_dir / "test-skill"
        skill_dir.mkdir()
        (skill_dir / "SKILL.md").write_text(sample_skill_content)

        skill = SkillLoader.load_skill(skill_dir)

        assert skill.metadata.name == "test-skill"
        assert skill.metadata.description == "A test skill for unit testing"
        assert skill.metadata.license == "MIT"
        assert skill.metadata.allowed_tools == ["bash", "python"]
        assert skill.metadata.metadata == {"author": "Test Author", "version": "1.0.0"}
        assert "This is a test skill with instructions" in skill.instructions

    def test_parse_skill_md_minimal(self, temp_skill_dir, minimal_skill_content):
        """Test parsing a minimal SKILL.md file."""
        skill_dir = temp_skill_dir / "minimal-skill"
        skill_dir.mkdir()
        (skill_dir / "SKILL.md").write_text(minimal_skill_content)

        skill = SkillLoader.load_skill(skill_dir)

        assert skill.metadata.name == "minimal-skill"
        assert skill.metadata.description == "Minimal test skill"
        assert skill.metadata.license is None
        assert skill.metadata.allowed_tools is None
        assert skill.metadata.metadata is None
        assert "Just the basics" in skill.instructions

    def test_missing_skill_md(self, temp_skill_dir):
        """Test error when SKILL.md is missing."""
        skill_dir = temp_skill_dir / "no-skill"
        skill_dir.mkdir()

        with pytest.raises(FileNotFoundError):
            SkillLoader.load_skill(skill_dir)

    def test_missing_required_fields(self, temp_skill_dir):
        """Test error when required fields are missing."""
        skill_dir = temp_skill_dir / "bad-skill"
        skill_dir.mkdir()

        # Missing description
        (skill_dir / "SKILL.md").write_text(
            """---
name: bad-skill
---

Content
"""
        )

        with pytest.raises(Exception):  # Should raise UserError
            SkillLoader.load_skill(skill_dir)

    def test_invalid_skill_name(self, temp_skill_dir):
        """Test error when skill name has invalid characters."""
        skill_dir = temp_skill_dir / "invalid-name"
        skill_dir.mkdir()

        (skill_dir / "SKILL.md").write_text(
            """---
name: Invalid_Name!
description: Bad name
---

Content
"""
        )

        with pytest.raises(Exception):  # Should raise UserError
            SkillLoader.load_skill(skill_dir)

    def test_discover_skills(self, temp_skill_dir):
        """Test discovering multiple skills."""
        # Create skill 1
        skill1_dir = temp_skill_dir / "skill-one"
        skill1_dir.mkdir()
        (skill1_dir / "SKILL.md").write_text(
            """---
name: skill-one
description: First skill
---

Skill one
"""
        )

        # Create skill 2
        skill2_dir = temp_skill_dir / "skill-two"
        skill2_dir.mkdir()
        (skill2_dir / "SKILL.md").write_text(
            """---
name: skill-two
description: Second skill
---

Skill two
"""
        )

        skills = SkillLoader.discover_skills(temp_skill_dir)

        assert len(skills) == 2
        skill_names = {s.metadata.name for s in skills}
        assert skill_names == {"skill-one", "skill-two"}


class TestSkill:
    """Tests for Skill class."""

    def test_read_file(self, temp_skill_dir, minimal_skill_content):
        """Test reading a bundled file from a skill."""
        skill_dir = temp_skill_dir / "test-skill"
        skill_dir.mkdir()
        (skill_dir / "SKILL.md").write_text(minimal_skill_content)

        # Create a reference file
        refs_dir = skill_dir / "references"
        refs_dir.mkdir()
        (refs_dir / "api.md").write_text("# API Documentation")

        skill = SkillLoader.load_skill(skill_dir)
        content = skill.read_file("references/api.md")

        assert content == "# API Documentation"

    def test_read_file_security(self, temp_skill_dir, minimal_skill_content):
        """Test that reading files outside skill directory is prevented."""
        skill_dir = temp_skill_dir / "test-skill"
        skill_dir.mkdir()
        (skill_dir / "SKILL.md").write_text(minimal_skill_content)

        skill = SkillLoader.load_skill(skill_dir)

        from agents.exceptions import UserError

        with pytest.raises(UserError):
            skill.read_file("../../../etc/passwd")

    def test_list_files(self, temp_skill_dir, minimal_skill_content):
        """Test listing files in a skill."""
        skill_dir = temp_skill_dir / "test-skill"
        skill_dir.mkdir()
        (skill_dir / "SKILL.md").write_text(minimal_skill_content)

        # Create various files
        scripts_dir = skill_dir / "scripts"
        scripts_dir.mkdir()
        (scripts_dir / "script1.py").write_text("print('hello')")
        (scripts_dir / "script2.py").write_text("print('world')")

        skill = SkillLoader.load_skill(skill_dir)
        files = skill.list_files("scripts/*.py")

        assert len(files) == 2
        assert "scripts/script1.py" in files
        assert "scripts/script2.py" in files

    def test_get_summary(self, temp_skill_dir, minimal_skill_content):
        """Test getting skill summary."""
        skill_dir = temp_skill_dir / "test-skill"
        skill_dir.mkdir()
        (skill_dir / "SKILL.md").write_text(minimal_skill_content)

        skill = SkillLoader.load_skill(skill_dir)
        summary = skill.get_summary()

        assert summary == "- [minimal-skill]: Minimal test skill"


class TestSkillRegistry:
    """Tests for SkillRegistry."""

    def test_register_and_get_skill(self, registry, temp_skill_dir, minimal_skill_content):
        """Test registering and retrieving a skill."""
        skill_dir = temp_skill_dir / "test-skill"
        skill_dir.mkdir()
        (skill_dir / "SKILL.md").write_text(minimal_skill_content)

        skill = SkillLoader.load_skill(skill_dir)
        registry.register_skill(skill)

        retrieved = registry.get_skill("minimal-skill")
        assert retrieved is not None
        assert retrieved.metadata.name == "minimal-skill"

    def test_register_skill_directory(self, registry, temp_skill_dir):
        """Test registering all skills from a directory."""
        # Create multiple skills
        for i in range(3):
            skill_dir = temp_skill_dir / f"skill-{i}"
            skill_dir.mkdir()
            (skill_dir / "SKILL.md").write_text(
                f"""---
name: skill-{i}
description: Skill number {i}
---

Skill {i}
"""
            )

        count = registry.register_skill_directory(temp_skill_dir)

        assert count == 3
        assert len(registry.get_all_skills()) == 3
        assert "skill-0" in registry.get_skill_names()
        assert "skill-1" in registry.get_skill_names()
        assert "skill-2" in registry.get_skill_names()

    def test_get_skills_summary(self, registry, temp_skill_dir):
        """Test getting summary of all skills."""
        skill_dir = temp_skill_dir / "test-skill"
        skill_dir.mkdir()
        (skill_dir / "SKILL.md").write_text(
            """---
name: test-skill
description: A test skill
---

Content
"""
        )

        registry.register_skill_directory(temp_skill_dir)
        summary = registry.get_skills_summary()

        assert "- [test-skill]: A test skill" in summary

    def test_get_skills_xml(self, registry, temp_skill_dir):
        """Test getting XML-formatted skills list."""
        skill_dir = temp_skill_dir / "test-skill"
        skill_dir.mkdir()
        (skill_dir / "SKILL.md").write_text(
            """---
name: test-skill
description: A test skill
---

Content
"""
        )

        registry.register_skill_directory(temp_skill_dir)
        xml = registry.get_skills_xml()

        assert "<available_skills>" in xml
        assert "<skill>" in xml
        assert "<name>test-skill</name>" in xml
        assert "<description>A test skill</description>" in xml
        assert "</available_skills>" in xml

    def test_clear_registry(self, registry, temp_skill_dir, minimal_skill_content):
        """Test clearing the registry."""
        skill_dir = temp_skill_dir / "test-skill"
        skill_dir.mkdir()
        (skill_dir / "SKILL.md").write_text(minimal_skill_content)

        registry.register_skill_directory(temp_skill_dir)
        assert len(registry.get_all_skills()) == 1

        registry.clear()
        assert len(registry.get_all_skills()) == 0


class TestSkillTool:
    """Tests for the Skill tool."""

    def test_skill_tool_success(self, registry, temp_skill_dir):
        """Test successfully invoking a skill via the tool."""
        skill_dir = temp_skill_dir / "demo-skill"
        skill_dir.mkdir()
        (skill_dir / "SKILL.md").write_text(
            """---
name: demo-skill
description: Demo skill
---

# Demo Instructions

Follow these demo instructions.
"""
        )

        registry.register_skill_directory(temp_skill_dir)

        # Get skill tool for this registry
        skill_tool = get_skill_tool(registry)

        # Create a mock context
        from agents.run_context import RunContextWrapper

        ctx = RunContextWrapper(context=None)

        # Invoke the skill tool function directly
        result = skill_tool.on_invoke_tool(
            ctx,  # type: ignore
            '{"command": "demo-skill"}',
        )

        # Since it's async, we need to await it
        import asyncio

        result_str = asyncio.run(result)

        assert "Base directory for this skill:" in result_str
        assert "# Demo Instructions" in result_str
        assert "Follow these demo instructions" in result_str

    def test_skill_tool_not_found(self, registry):
        """Test error when skill is not found."""
        # Get skill tool for this registry (with no skills registered)
        skill_tool = get_skill_tool(registry)

        from agents.run_context import RunContextWrapper

        ctx = RunContextWrapper(context=None)

        # Invoke the skill tool function directly
        import asyncio

        result = asyncio.run(
            skill_tool.on_invoke_tool(
                ctx,  # type: ignore
                '{"command": "nonexistent-skill"}',
            )
        )

        assert "Error" in result
        assert "not found" in result


class TestSkillsIntegration:
    """Integration tests for skills with agents."""

    def test_agent_with_skill_tool(self, registry, temp_skill_dir):
        """Test creating an agent with the Skill tool."""
        # Create a test skill
        skill_dir = temp_skill_dir / "test-skill"
        skill_dir.mkdir()
        (skill_dir / "SKILL.md").write_text(
            """---
name: test-skill
description: Test skill for integration
---

# Test Skill

This is a test skill.
"""
        )

        registry.register_skill_directory(temp_skill_dir)

        # Get skill tool for this registry
        skill_tool = get_skill_tool(registry)

        # Create agent with skill support
        instructions = """You are a test assistant.

Use skills when appropriate."""

        agent = Agent(name="Test Assistant", instructions=instructions, tools=[skill_tool])

        # Verify agent has the skill tool
        assert len(agent.tools) == 1
        assert agent.tools[0].name == "Skill"
