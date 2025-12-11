import json
import shutil
import tempfile
import textwrap
from pathlib import Path

import pytest

from kagent.skills import (
    discover_skills,
    execute_command,
    load_skill_content,
    read_file_content,
)


@pytest.fixture
def skill_test_env() -> Path:
    """
    Creates a temporary environment that mimics a real session and ensures cleanup.

    This fixture manually creates and deletes the temporary directory structure
    to guarantee that no files are left behind after the test run.
    """
    # 1. Create a single top-level temporary directory
    top_level_dir = Path(tempfile.mkdtemp())

    try:
        session_dir = top_level_dir / "session"
        skills_root_dir = top_level_dir / "skills_root"

        # 2. Create session directories
        (session_dir / "uploads").mkdir(parents=True, exist_ok=True)
        (session_dir / "outputs").mkdir(parents=True, exist_ok=True)

        # 3. Create the skill to be tested
        skill_dir = skills_root_dir / "csv-to-json"
        script_dir = skill_dir / "scripts"
        script_dir.mkdir(parents=True, exist_ok=True)

        # SKILL.md
        (skill_dir / "SKILL.md").write_text(
            textwrap.dedent("""\
---
            name: csv-to-json
            description: Converts a CSV file to a JSON file.
            ---
            # CSV to JSON Conversion
            Use the `convert.py` script to convert a CSV file from the `uploads` directory
            to a JSON file in the `outputs` directory.
            Example: `bash("python skills/csv-to-json/scripts/convert.py uploads/data.csv outputs/result.json")`
        """)
        )

        # Python script for the skill
        (script_dir / "convert.py").write_text(
            textwrap.dedent("""
            import csv
            import json
            import sys
            if len(sys.argv) != 3:
                print(f"Usage: python {sys.argv[0]} <input_csv> <output_json>")
                sys.exit(1)
            input_path, output_path = sys.argv[1], sys.argv[2]
            try:
                data = []
                with open(input_path, 'r', encoding='utf-8') as f:
                    reader = csv.DictReader(f)
                    for row in reader:
                        data.append(row)
                with open(output_path, 'w', encoding='utf-8') as f:
                    json.dump(data, f, indent=2)
                print(f"Successfully converted {input_path} to {output_path}")
            except FileNotFoundError:
                print(f"Error: Input file not found at {input_path}")
                sys.exit(1)
        """)
        )

        # 4. Create a symlink from the session to the skills root
        (session_dir / "skills").symlink_to(skills_root_dir, target_is_directory=True)

        # 5. Yield the session directory path to the test
        yield session_dir

    finally:
        # 6. Explicitly clean up the entire temporary directory
        shutil.rmtree(top_level_dir)


@pytest.mark.asyncio
async def test_skill_core_logic(skill_test_env: Path):
    """
    Tests the core logic of the 'csv-to-json' skill by directly
    calling the centralized tool functions.
    """
    session_dir = skill_test_env

    # 1. "Upload" a file for the skill to process
    input_csv_path = session_dir / "uploads" / "data.csv"
    input_csv_path.write_text("id,name\n1,Alice\n2,Bob\n")

    # 2. Execute the skill's core command, just as an agent would
    # We use the centralized `execute_command` function directly
    command = "python skills/csv-to-json/scripts/convert.py uploads/data.csv outputs/result.json"
    result = await execute_command(command, working_dir=session_dir)

    assert "Successfully converted" in result

    # 3. Verify the output by reading the generated file
    # We use the centralized `read_file_content` function directly
    output_json_path = session_dir / "outputs" / "result.json"

    # The read_file_content function returns a string with line numbers,
    # so we need to parse it.
    raw_output = read_file_content(output_json_path)
    json_content_str = "\n".join(line.split("|", 1)[1] for line in raw_output.splitlines())

    # Assert the content is correct
    expected_data = [{"id": "1", "name": "Alice"}, {"id": "2", "name": "Bob"}]
    assert json.loads(json_content_str) == expected_data


def test_skill_discovery_and_loading(skill_test_env: Path):
    """
    Tests the core logic of discovering a skill and loading its instructions.
    """
    # The fixture creates the session dir, the skills are one level up in a separate dir
    skills_root_dir = skill_test_env.parent / "skills_root"

    # 1. Test skill discovery
    discovered = discover_skills(skills_root_dir)
    assert len(discovered) == 1
    skill_meta = discovered[0]
    assert skill_meta.name == "csv-to-json"
    assert "Converts a CSV file" in skill_meta.description

    # 2. Test skill content loading
    skill_content = load_skill_content(skills_root_dir, "csv-to-json")
    assert "name: csv-to-json" in skill_content
    assert "# CSV to JSON Conversion" in skill_content
    assert 'Example: `bash("python skills/csv-to-json/scripts/convert.py' in skill_content
