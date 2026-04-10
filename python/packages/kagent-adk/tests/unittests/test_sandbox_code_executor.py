from types import SimpleNamespace
from unittest.mock import patch

from google.adk.code_executors.code_execution_utils import CodeExecutionInput

from kagent.adk.sandbox_code_executer import SandboxedLocalCodeExecutor


def test_execute_code_uses_session_working_directory():
    executor = SandboxedLocalCodeExecutor()
    invocation_context = SimpleNamespace(session=SimpleNamespace(id="session-123"))
    code_input = CodeExecutionInput(code="print('ok')")

    with (
        patch("kagent.adk.sandbox_code_executer.get_session_path", return_value="/tmp/kagent/session-123"),
        patch(
            "kagent.adk.sandbox_code_executer._get_srt_settings_args",
            return_value=["--settings", "/config/srt-settings.json"],
        ),
        patch("kagent.adk.sandbox_code_executer._sanitize_env", return_value={}),
        patch("kagent.adk.sandbox_code_executer.subprocess.run") as mock_run,
    ):
        mock_run.return_value = SimpleNamespace(stdout="ok\n", stderr="")
        result = executor.execute_code(invocation_context, code_input)

    assert result.stdout == "ok\n"
    assert result.stderr == ""
    _, kwargs = mock_run.call_args
    assert kwargs["cwd"] == "/tmp/kagent/session-123"
