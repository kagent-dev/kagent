from datetime import datetime
from typing import Any, Dict

from fastapi import APIRouter, Depends, HTTPException
from loguru import logger
from pydantic import BaseModel, Field

from ...database.db_manager import DatabaseManager
from ...datamodel import Feedback, Response
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
    timestamp: str = Field(None, description="Timestamp of the feedback submission")
    sessionID: int = Field(None, description="Session ID")
    userID: str = Field(None, description="User ID")


@router.post("/", response_model=Response)
async def create_feedback(
    request: FeedbackSubmissionRequest,
    db=Depends(get_db),
) -> Response:
    """
    Create a new feedback entry from user feedback on agent responses

    Args:
        request: The feedback data from the client
        user_id: The ID of the user submitting the feedback
        db_manager: Database manager instance

    Returns:
        Response: Result of the operation with status and message
    """

    # Convert to dict for DB manager
    feedback_dict = request.model_dump()

    # Create feedback in database
    response = await _create_feedback(db, feedback_dict)

    if not response.status:
        logger.error(f"Failed to create feedback: {response.message}")
        raise HTTPException(status_code=500, detail=response.message)

    return response


@router.get("/", response_model=dict)
async def list_feedback(
    user_id: str,
    db=Depends(get_db),
):
    """
    List all feedback entries for a given user

    Args:
        user_id: The ID of the user to list feedback for
        db: The database manager instance

    Returns:
        Response: Result of the operation with status and message
    """
    try:
        result = db.get(Feedback, filters={"user_id": user_id})
        return { "status": True, "data": result.data }
    except Exception as e:
        logger.error(f"Error listing feedback: {str(e)}")
        return { "status": False, "message": str(e) }

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
            preceding_messages=feedback_data.get("precedingMessagesContents", []),
            user_id=feedback_data.get("userID"),
            session_id=feedback_data.get("sessionID"),
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
