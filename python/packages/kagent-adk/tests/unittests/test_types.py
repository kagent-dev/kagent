from kagent.adk.types import AgentConfig, AskUserConfig, GeminiVertexAI


def test_ask_user_enabled():
    """Verify that AskUserTool is added when ask_user.enabled is true."""
    config = AgentConfig(
        model=GeminiVertexAI(model="gemini-pro"),
        description="Test Agent",
        instruction="You are a test agent.",
        ask_user=AskUserConfig(enabled=True),
    )
    agent = config.to_agent(name="test-agent")
    assert any(
        tool.name == "ask_user" for tool in agent.tools
    ), "AskUserTool should be present when enabled"


def test_ask_user_disabled():
    """Verify that AskUserTool is not added when ask_user.enabled is false."""
    config = AgentConfig(
        model=GeminiVertexAI(model="gemini-pro"),
        description="Test Agent",
        instruction="You are a test agent.",
        ask_user=AskUserConfig(enabled=False),
    )
    agent = config.to_agent(name="test-agent")
    assert not any(
        tool.name == "ask_user" for tool in agent.tools
    ), "AskUserTool should not be present when disabled"


def test_ask_user_not_specified():
    """Verify that AskUserTool is not added when ask_user is not specified."""
    config = AgentConfig(
        model=GeminiVertexAI(model="gemini-pro"),
        description="Test Agent",
        instruction="You are a test agent.",
    )
    agent = config.to_agent(name="test-agent")
    assert not any(
        tool.name == "ask_user" for tool in agent.tools
    ), "AskUserTool should not be present when not specified"
