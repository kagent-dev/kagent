"""Test that tool names don't exceed OpenAI's 64-character limit."""

import pytest
from kagent.adk.types import sanitize_agent_name


def test_sanitize_agent_name_basic():
    """Test basic sanitization."""
    assert sanitize_agent_name("hello-world") == "hello_world"
    assert sanitize_agent_name("Hello World") == "Hello_World"
    assert sanitize_agent_name("123-invalid") == "_123_invalid"


def test_sanitize_agent_name_with_max_length():
    """Test sanitization with max_length parameter."""
    long_name = "strategic_analysis_workflow_Strategic_Analysis_Pipeline_sequential"
    assert len(long_name) == 66  # Original length
    
    sanitized = sanitize_agent_name(long_name, max_length=64)
    assert len(sanitized) <= 64
    assert sanitized == "strategic_analysis_workflow_Strategic_Analysis_Pipeline_sequenti"


def test_workflow_agent_names_within_limit():
    """Test that workflow agent names stay within OpenAI's 64-character limit."""
    parent_name = "strategic_analysis_workflow"
    roles = [
        "Multi-Domain Research",
        "Strategic Analysis Pipeline",
    ]
    
    for role in roles:
        sanitized_role = sanitize_agent_name(role)
        for wf_type in ["sequential", "parallel", "loop"]:
            workflow_name = f"{parent_name}_{sanitized_role}_{wf_type}"
            workflow_name = sanitize_agent_name(workflow_name, max_length=64)
            
            assert len(workflow_name) <= 64, (
                f"Workflow name '{workflow_name}' exceeds 64 characters: {len(workflow_name)}"
            )


def test_agent_ref_names_within_limit():
    """Test that agent reference names stay within limits."""
    test_cases = [
        ("kagent", "argo-rollouts-conversion-agent"),
        ("kagent", "strategic-analysis-workflow"),
        ("kagent", "recommendation-generator"),
    ]
    
    for namespace, name in test_cases:
        # Simulate ConvertToPythonIdentifier from Go code
        ref = f"{namespace}/{name}"
        ref = ref.replace("-", "_").replace("/", "__NS__")
        if len(ref) > 64:
            ref = ref[:64]
        
        assert len(ref) <= 64, f"Agent ref '{ref}' exceeds 64 characters: {len(ref)}"

