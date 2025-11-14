# Copyright 2025 Google LLC
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

from __future__ import annotations

import subprocess

from pydantic import Field
from typing_extensions import override

from google.adk.agents.invocation_context import InvocationContext
from google.adk.code_executors.base_code_executor import BaseCodeExecutor
from google.adk.code_executors.code_execution_utils import CodeExecutionInput
from google.adk.code_executors.code_execution_utils import CodeExecutionResult


class SandboxedLocalCodeExecutor(BaseCodeExecutor):
    """A code executor that execute code in a sandbox in the current local context."""

    # Overrides the BaseCodeExecutor attribute: this executor cannot be stateful.
    stateful: bool = Field(default=False, frozen=True, exclude=True)

    # Overrides the BaseCodeExecutor attribute: this executor cannot
    # optimize_data_file.
    optimize_data_file: bool = Field(default=False, frozen=True, exclude=True)

    def __init__(self, **data):
        """Initializes the SandboxedLocalCodeExecutor."""
        if "stateful" in data and data["stateful"]:
            raise ValueError("Cannot set `stateful=True` in SandboxedLocalCodeExecutor.")
        if "optimize_data_file" in data and data["optimize_data_file"]:
            raise ValueError("Cannot set `optimize_data_file=True` in SandboxedLocalCodeExecutor.")
        super().__init__(**data)

    @override
    def execute_code(
        self,
        invocation_context: InvocationContext,
        code_execution_input: CodeExecutionInput,
    ) -> CodeExecutionResult:
        """Executes the given code in a sandboxed local context. uses the srt command to sandbox"""
        output = ""
        error = ""

        try:
            # Execute the provided code by piping it to `python -` inside the sandbox.
            proc = subprocess.run(
                ["srt", "python", "-"],
                input=code_execution_input.code,
                capture_output=True,
                text=True,
            )
            output = proc.stdout or ""
            error = proc.stderr or ""
        except FileNotFoundError as e:
            # srt or python not found
            output = ""
            error = f"Execution failed: {e}"
        except Exception as e:
            output = ""
            error = f"Unexpected error during execution: {e}"

        # Collect the final result.
        return CodeExecutionResult(
            stdout=output,
            stderr=error,
            output_files=[],
        )
