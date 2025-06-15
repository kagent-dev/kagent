# api/routes/sessions.py
import json
from typing import Dict, Union

from fastapi import APIRouter, Depends, HTTPException
from fastapi.responses import StreamingResponse
from loguru import logger
from pydantic import BaseModel

from ...database import DatabaseManager
from ...datamodel import Message, MessageConfig, Response, Run, RunStatus, Session, TeamResult
from ...sessionmanager import SessionManager
from ..deps import get_db, get_session_manager
from .invoke import format_team_result
from autogen_core import ComponentModel

router = APIRouter()


@router.get("/")
async def list_sessions(user_id: str, db=Depends(get_db)) -> Dict:
    """List all sessions for a user"""
    response = db.get(Session, filters={"user_id": user_id})
    return {"status": True, "data": response.data}


@router.get("/{session_id}")
async def get_session(session_id: int, user_id: str, db=Depends(get_db)) -> Dict:
    """Get a specific session"""
    response = db.get(Session, filters={"id": session_id, "user_id": user_id})
    if not response.status or not response.data:
        raise HTTPException(status_code=404, detail="Session not found")
    return {"status": True, "data": response.data[0]}


@router.post("/")
async def create_session(session: Session, db=Depends(get_db)) -> Response:
    """Create a new session"""
    try:
        response = db.upsert(session)
        if not response.status:
            return Response(status=False, message=f"Failed to create session: {response.message}")
        return Response(status=True, data=response.data, message="Session created successfully")
    except Exception as e:
        logger.error(f"Error creating session: {str(e)}")
        return Response(status=False, message=f"Failed to create session: {str(e)}")


@router.put("/{session_id}")
async def update_session(session_id: int, user_id: str, session: Session, db=Depends(get_db)) -> Dict:
    """Update an existing session"""
    # First verify the session belongs to user
    existing_response = db.get(Session, filters={"id": session_id, "user_id": user_id}, return_json=False)
    if not existing_response.status or not existing_response.data:
        raise HTTPException(status_code=404, detail="Session not found")

    # Get the existing session
    existing_session = existing_response.data[0]
    existing_session.name = session.name

    try:
        response = db.upsert(existing_session)
        if not response.status:
            raise HTTPException(status_code=400, detail=response.message)

        return {"status": True, "data": response.data, "message": "Session updated successfully"}
    except Exception as e:
        logger.error(f"Error updating session name: {str(e)}")
        raise HTTPException(status_code=400, detail=f"Invalid session data: {str(e)}") from e


@router.delete("/{session_id}")
async def delete_session(session_id: int, user_id: str, db=Depends(get_db)) -> Dict:
    """Delete a session"""
    db.delete(filters={"id": session_id, "user_id": user_id}, model_class=Session)
    return {"status": True, "message": "Session deleted successfully"}


@router.get("/{session_id}/runs")
async def list_session_runs(session_id: int, user_id: str, db: DatabaseManager = Depends(get_db)) -> Dict:
    """Get complete session history organized by runs"""

    try:
        # 1. Verify session exists and belongs to user
        session = db.get(Session, filters={"id": session_id, "user_id": user_id}, return_json=False)
        if not session.status:
            raise HTTPException(status_code=500, detail="Database error while fetching session")
        if not session.data:
            raise HTTPException(status_code=404, detail="Session not found or access denied")

        # 2. Get ordered runs for session
        runs = db.get(Run, filters={"session_id": session_id}, order="asc", return_json=False)
        if not runs.status:
            raise HTTPException(status_code=500, detail="Database error while fetching runs")

        # 3. Build response with messages per run
        run_data = []
        if runs.data:  # It's ok to have no runs
            for run in runs.data:
                try:
                    # Get messages for this specific run
                    messages = db.get(Message, filters={"run_id": run.id}, order="asc", return_json=False)
                    if not messages.status:
                        logger.error(f"Failed to fetch messages for run {run.id}")
                        # Continue processing other runs even if one fails
                        messages.data = []

                    run_data.append(
                        {
                            "id": run.id,
                            "created_at": run.created_at,
                            "status": run.status,
                            "task": run.task,
                            "team_result": run.team_result,
                            "messages": messages.data or [],
                        }
                    )
                except Exception as e:
                    logger.error(f"Error processing run {run.id}: {str(e)}")
                    # Include run with error state instead of failing entirely
                    run_data.append(
                        {
                            "id": run.id,
                            "created_at": run.created_at,
                            "status": "ERROR",
                            "task": run.task,
                            "team_result": None,
                            "messages": [],
                            "error": f"Failed to process run: {str(e)}",
                        }
                    )

        return {"status": True, "data": {"runs": run_data}}

    except HTTPException:
        raise  # Re-raise HTTP exceptions
    except Exception as e:
        logger.error(f"Unexpected error in list_messages: {str(e)}")
        raise HTTPException(status_code=500, detail="Internal server error while fetching session data") from e


class InvokeRequest(BaseModel):
    task: str
    team_config: Union[ComponentModel, dict]


@router.post("/{session_id}/invoke")
async def invoke(
    session_id: int,
    user_id: str,
    request: InvokeRequest,
    db: DatabaseManager = Depends(get_db),
    session_mgr: SessionManager = Depends(get_session_manager),
) -> Response:
    try:
        run = _create_run(session_id, user_id, db, request.task)
        result: TeamResult = await session_mgr.start(user_id, run.id, request.task, request.team_config)
        response = Response(status=True, data=format_team_result(result), message="Run executed successfully")
        return response

    except Exception as e:
        logger.error(f"Error invoking run: {str(e)}")
        raise HTTPException(status_code=500, detail=f"Internal server error while invoking run: {str(e)}") from e


def _create_run(session_id: int, user_id: str, db: DatabaseManager, task: str) -> Run:
    run = Run(
        session_id=session_id,
        user_id=user_id,
        status=RunStatus.CREATED,
        task=MessageConfig(
            content=task,
            source="user",
        ).model_dump(),
        team_result={},
    )
    response: Response = db.upsert(run, return_json=False)
    if not response.status or not response.data:
        raise HTTPException(status_code=500, detail="Failed to create run")
    return response.data


@router.post("/{session_id}/invoke/stream")
async def stream(
    session_id: int,
    user_id: str,
    request: InvokeRequest,
    db: DatabaseManager = Depends(get_db),
    session_mgr: SessionManager = Depends(get_session_manager),
):
    async def event_generator():
        try:
            # Create a new run
            run = _create_run(session_id, user_id, db, request.task)
            # Start the run
            async for event in session_mgr.start_stream(user_id, run.id, request.task, request.team_config):
                if "task_result" in event:
                    yield f"event: task_result\ndata: {json.dumps(event)}\n\n"
                else:
                    yield f"event: event\ndata: {json.dumps(event)}\n\n"
            yield f"event: completion\ndata: {json.dumps({'type': 'completion', 'status': 'success', 'data': None})}\n\n"
        except Exception as e:
            logger.error(f"Error during SSE stream generation: {e}", exc_info=True)
            error_payload = {"type": "error", "data": {"message": str(e), "details": type(e).__name__}}
            yield f"data: {json.dumps(error_payload)}\n\n"

    return StreamingResponse(event_generator(), media_type="text/event-stream")
