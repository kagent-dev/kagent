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

from typing import Optional

from pydantic import BaseModel


class Skill(BaseModel):
    """Represents the metadata for a skill.

    This is a simple data container used during the initial skill discovery
    phase to hold the information parsed from a skill's SKILL.md frontmatter.
    """

    name: str
    """The unique name/identifier of the skill."""

    description: str
    """A description of what the skill does and when to use it."""

    license: Optional[str] = None
    """Optional license information for the skill."""
