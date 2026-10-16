import tempfile
from pathlib import Path

import pytest

from kagent.adk.artifacts import clear_session_cache, get_session_path, initialize_session_path


@pytest.fixture(autouse=True)
def isolated_sessions(tmp_path: Path, monkeypatch):
    monkeypatch.setattr(tempfile, "tempdir", str(tmp_path / "sessions"))
    clear_session_cache()
    yield
    clear_session_cache()


@pytest.mark.parametrize("directory", ["./skills", "nested/skills", "../skills", "absolute"])
def test_session_skills_are_accessible(directory, tmp_path: Path, monkeypatch):
    project = tmp_path / "project"
    project.mkdir()
    skills_dir = project / (directory if directory != "absolute" else "skills")
    skill_file = skills_dir / "example" / "SKILL.md"
    skill_file.parent.mkdir(parents=True)
    skill_file.write_text("Skill instructions\n", encoding="utf-8")
    monkeypatch.chdir(project)
    skills_directory = str(skills_dir) if directory == "absolute" else directory

    session_path = initialize_session_path("test-session", skills_directory)
    skills_link = session_path / "skills"

    assert skills_link.is_symlink()
    assert (skills_link / "example" / "SKILL.md").read_text(encoding="utf-8") == "Skill instructions\n"
    assert skills_link.readlink() == skills_dir.absolute()
    assert (session_path / "uploads").is_dir()
    assert (session_path / "outputs").is_dir()

    # A cached session keeps its link even when the caller changes directory.
    monkeypatch.chdir(tmp_path)
    assert initialize_session_path("test-session", skills_directory) == session_path
    assert get_session_path("test-session") == session_path
    assert (skills_link / "example" / "SKILL.md").read_text(encoding="utf-8") == "Skill instructions\n"


def test_session_preserves_existing_skills_link(tmp_path: Path):
    original_skills = tmp_path / "original"
    original_skills.mkdir()
    (original_skills / "SKILL.md").write_text("Original skill\n", encoding="utf-8")
    new_skills = tmp_path / "new"
    new_skills.mkdir()
    session_path = tmp_path / "sessions" / "kagent" / "existing-session"
    session_path.mkdir(parents=True)
    (session_path / "skills").symlink_to(original_skills)

    assert initialize_session_path("existing-session", str(new_skills)) == session_path.resolve()
    assert (session_path / "skills").readlink() == original_skills
    assert (session_path / "skills" / "SKILL.md").read_text(encoding="utf-8") == "Original skill\n"


def test_session_without_skills_directory(tmp_path: Path):
    session_path = initialize_session_path("missing-skills", str(tmp_path / "missing"))

    assert (session_path / "uploads").is_dir()
    assert (session_path / "outputs").is_dir()
    assert not (session_path / "skills").is_symlink()


@pytest.mark.parametrize("relative", [False, True])
def test_session_skills_directory_symlink_can_be_retargeted(relative, tmp_path: Path, monkeypatch):
    original_skills = tmp_path / "original"
    original_skills.mkdir()
    new_skills = tmp_path / "new"
    new_skills.mkdir()
    (original_skills / "SKILL.md").write_text("Original skill\n", encoding="utf-8")
    (new_skills / "SKILL.md").write_text("New skill\n", encoding="utf-8")
    skills_dir = tmp_path / "skills"
    skills_dir.symlink_to(original_skills)
    monkeypatch.chdir(tmp_path)

    session_path = initialize_session_path("linked-skills", "skills" if relative else str(skills_dir))
    skills_link = session_path / "skills"
    assert (skills_link / "SKILL.md").read_text(encoding="utf-8") == "Original skill\n"
    skills_dir.unlink()
    skills_dir.symlink_to(new_skills)
    assert (skills_link / "SKILL.md").read_text(encoding="utf-8") == "New skill\n"
