from datetime import datetime
from typing import Any, Dict

from fastapi import APIRouter, Depends, HTTPException
from loguru import logger
from pydantic import BaseModel, Field

from ...datamodel import Feedback, Response
from ...database.db_manager import DatabaseManager
from ..deps import get_db

router = APIRouter()


class FeedbackSubmissionRequest(BaseModel):
    """Model for feedback submission requests"""

    isPositive: bool = Field(description="Whether the feedback is positive")
    feedbackText: str = Field(description="The feedback text provided by the user")
    issueType: str = Field(None, description="The type of issue for negative feedback")
    messageContent: str = Field(description="Content of the message that received feedback")
    messageSource: str = Field(description="Source of the message (agent name)")
    precedingMessagesContents: list[str] = Field([], description="Contents of messages preceding the feedback")
    sessionInfo: str = Field(None, description="Session information")
    timestamp: str = Field(None, description="Timestamp of the feedback submission")
    clientInfo: Dict[str, Any] = Field({}, description="Client information")


@router.post("/feedback", response_model=Response)
async def create_feedback(
    feedback_data: FeedbackSubmissionRequest,
    db=Depends(get_db),
):
    """
    Create a new feedback entry from user feedback on agent responses

    Args:
        feedback_data: The feedback data from the client
        user_id: The ID of the user submitting the feedback
        db_manager: Database manager instance

    Returns:
        Response: Result of the operation with status and message
    """

    # Add client information (can be expanded)
    if not feedback_data.clientInfo:
        feedback_data.clientInfo = {"timestamp": feedback_data.timestamp}

    # Convert to dict for DB manager
    feedback_dict = feedback_data.model_dump()

    # Create feedback in database
    response = await _create_feedback(db, feedback_dict)

    if not response.status:
        logger.error(f"Failed to create feedback: {response.message}")
        raise HTTPException(status_code=500, detail=response.message)

    return response


async def _create_feedback(db: DatabaseManager, feedback_data: dict) -> Response:
    """
    Create a new feedback entry in the database

    Args:
        feedback_data (dict): The feedback data from the client

    Returns:
        Response: Result of the operation
    """
    try:
        # Create a new Feedback object
        feedback = Feedback(
            is_positive=feedback_data.get("isPositive", False),
            feedback_text=feedback_data.get("feedbackText", ""),
            issue_type=feedback_data.get("issueType"),
            message_content=feedback_data.get("messageContent", ""),
            message_source=feedback_data.get("messageSource", ""),
            # Store preceding messages as a list
            preceding_messages=feedback_data.get("precedingMessagesContents", []),
            # Add additional metadata
            extra_metadata={
                "timestamp": feedback_data.get("timestamp", datetime.now().isoformat()),
                "session_info": feedback_data.get("sessionInfo"),
                "client_info": feedback_data.get("clientInfo", {}),
            },
        )

        # Try to get session ID if available
        session_info = feedback_data.get("sessionInfo")
        if session_info and isinstance(session_info, str) and session_info.isdigit():
            feedback.session_id = int(session_info)

        # Save to database
        return db.upsert(feedback)

    except Exception as e:
        error_msg = f"Error creating feedback: {str(e)}"
        logger.error(error_msg)
        return Response(message=error_msg, status=False)
