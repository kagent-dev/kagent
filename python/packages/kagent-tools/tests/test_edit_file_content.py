"""edit_file_content must change only the text it was asked to replace."""

from pathlib import Path

from kagent.tools import edit_file_content


def _edit(tmp_path: Path, raw: bytes, old_string: str, new_string: str, replace_all: bool = False) -> bytes:
    path = tmp_path / "target.txt"
    path.write_bytes(raw)
    edit_file_content(path, old_string, new_string, replace_all)
    return path.read_bytes()


def test_single_line_edit_keeps_crlf_line_endings(tmp_path: Path):
    edited = _edit(tmp_path, b"a = 1\r\nb = 2\r\nc = 3\r\n", "b = 2", "b = 20")

    assert edited == b"a = 1\r\nb = 20\r\nc = 3\r\n"


def test_multi_line_edit_matches_a_crlf_file_with_lf_strings(tmp_path: Path):
    # read_file shows lines without their "\r", so the model writes its strings with "\n".
    edited = _edit(tmp_path, b"one\r\ntwo\r\nthree\r\nfour\r\n", "two\nthree", "TWO\nTHREE\nextra")

    assert edited == b"one\r\nTWO\r\nTHREE\r\nextra\r\nfour\r\n"


def test_replace_all_keeps_crlf_line_endings(tmp_path: Path):
    edited = _edit(tmp_path, b"x\r\ny\r\nx\r\n", "x", "z", replace_all=True)

    assert edited == b"z\r\ny\r\nz\r\n"


def test_mixed_line_endings_outside_the_edit_are_left_alone(tmp_path: Path):
    edited = _edit(tmp_path, b"a\r\nb\nc\r\n", "c", "C")

    assert edited == b"a\r\nb\nC\r\n"


def test_lf_file_stays_lf(tmp_path: Path):
    edited = _edit(tmp_path, b"a\nb\nc\n", "b\nc", "B\nC")

    assert edited == b"a\nB\nC\n"
