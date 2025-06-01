from datetime import datetime
from typing import Any, Dict, Optional

from fastapi import APIRouter, Depends, HTTPException
from loguru import logger
from pydantic import BaseModel, Field

from ...database.db_manager import DatabaseManager
from ...datamodel import Feedback, Response
from ..deps import get_db

router = APIRouter()


class FeedbackSubmissionRequest(BaseModel):
    """Model for feedback submission requests"""

    is_positive: bool = Field(description="Whether the feedback is positive")
    feedback_text: str = Field(description="The feedback text provided by the user")
    issue_type: Optional[str] = Field(None, description="The type of issue for negative feedback")
    user_id: Optional[str] = Field(None, description="User ID of the submitter")
    message_id: Optional[int] = Field(None, description="ID of the message this feedback pertains to")


@router.post("/", response_model=Response)
async def create_feedback(
    request: FeedbackSubmissionRequest,
    db: DatabaseManager = Depends(get_db),
) -> Response:
    """
    Create a new feedback entry from user feedback on agent responses

    Args:
        request: The feedback data from the client
        db: Database manager instance

    Returns:
        Response: Result of the operation with status and message
    """

    response = None
    try:
        response = await _create_feedback(db, request)
    except Exception as e:
        logger.error(f"Unexpected error creating feedback: {str(e)}")
        raise HTTPException(
            status_code=500, detail="An unexpected error occurred while processing your feedback."
        ) from e

    if not response.status:
        raise HTTPException(status_code=500, detail=response.message or "Failed to create feedback.")

    return response


@router.get("/", response_model=dict)
async def list_feedback(
    user_id: str,
    db: DatabaseManager = Depends(get_db),
):
    """
    List all feedback entries for a given user

    Args:
        user_id: The ID of the user to list feedback for
        db: The database manager instance

    Returns:
        dict: A dictionary containing the status and feedback data, or an error message.
    """
    try:
        result = db.get(Feedback, filters={"user_id": user_id})
        if result.status:
            return {"status": True, "data": result.data}
        else:
            logger.error(f"Error listing feedback from DB: {result.message}")
            raise HTTPException(status_code=500, detail=result.message or "Failed to retrieve feedback.")
    except HTTPException:
        raise
    except Exception as e:
        logger.error(f"Error listing feedback for user {user_id}: {str(e)}")
        raise HTTPException(status_code=500, detail="An error occurred while listing feedback.") from e


async def _create_feedback(db: DatabaseManager, feedback_data: FeedbackSubmissionRequest) -> Response:
    """
    Create a new feedback entry in the database

    Args:
        feedback_data (dict): The feedback data from the client
    Returns:
        Response: Result of the operation
    """
    try:
        feedback = Feedback(
            is_positive=feedback_data.is_positive,
            feedback_text=feedback_data.feedback_text,
            issue_type=feedback_data.issue_type,
            user_id=feedback_data.user_id,
            message_id=feedback_data.message_id,
        )
        return db.upsert(feedback)

    except Exception as e:
        error_msg = f"Error creating feedback: {str(e)}"
        logger.error(error_msg)
        return Response(message=error_msg, status=False)
