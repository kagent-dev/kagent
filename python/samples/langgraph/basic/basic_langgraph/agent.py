"""Basic LangGraph Agent for KAgent Integration.

This module demonstrates a simple LangGraph agent that can answer questions
and perform basic reasoning tasks using Google's Gemini model.
"""

from typing import Annotated, Sequence, TypedDict

from langchain_core.messages import BaseMessage
from langchain_google_genai import ChatGoogleGenerativeAI
from langgraph.graph import StateGraph, START, END
from langgraph.graph.message import add_messages
from langgraph.prebuilt import create_react_agent

from kagent_langgraph import KAgentApp


class State(TypedDict):
    """The state of our agent conversation."""

    messages: Annotated[Sequence[BaseMessage], add_messages]


def create_model():
    """Create and configure the language model."""
    return ChatGoogleGenerativeAI(
        model="gemini-2.0-flash-exp",
        temperature=0.7,
        max_tokens=1000,
    )


def chatbot_node(state: State) -> State:
    """The main chatbot node that processes messages and generates responses."""
    model = create_model()

    # Get the conversation history
    messages = state["messages"]

    # Generate response
    response = model.invoke(messages)

    # Return updated state
    return {"messages": [response]}


def create_graph() -> StateGraph:
    """Create the LangGraph workflow."""
    # Create the state graph
    graph = StateGraph(State)

    # Add nodes
    graph.add_node("chatbot", chatbot_node)

    # Add edges
    graph.add_edge(START, "chatbot")
    graph.add_edge("chatbot", END)

    return graph


# Create the KAgent application
root_app = KAgentApp(
    graph_builder=create_graph(),
    agent_card={
        "name": "basic-langgraph",
        "description": "A basic LangGraph agent that can answer questions and have conversations",
        "url": "localhost:8080",
        "version": "0.1.0",
        "capabilities": {"streaming": True},
        "defaultInputModes": ["text"],
        "defaultOutputModes": ["text"],
        "skills": [
            {
                "id": "conversation",
                "name": "Conversation",
                "description": "Can have natural conversations and answer questions",
                "tags": ["chat", "qa", "conversation"],
            }
        ],
    },
    kagent_url="http://localhost:8080",
    app_name="basic-langgraph-agent",
)
