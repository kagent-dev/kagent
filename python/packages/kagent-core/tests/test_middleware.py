"""Tests for the user_id extraction middleware."""

import pytest
from starlette.applications import Starlette
from starlette.requests import Request
from starlette.responses import JSONResponse
from starlette.testclient import TestClient

from kagent.core.a2a import USER_ID_HEADER, UserIdExtractionMiddleware, get_current_user_id


class TestUserIdExtractionMiddleware:
    """Test the create_user_id_extraction_middleware function."""

    def test_middleware_extracts_user_id_from_header(self):
        """Test that middleware extracts and sets user_id from X-User-ID header."""
        captured_user_id = None

        # Add a route that captures the user_id from context
        async def test_route(request: Request):
            nonlocal captured_user_id
            captured_user_id = get_current_user_id()
            return JSONResponse({"user_id": captured_user_id})

        # Create a simple Starlette app
        app = Starlette()

        # Add middleware
        app.add_middleware(UserIdExtractionMiddleware)

        # Add route
        app.add_route("/test", test_route, methods=["GET"])

        # Create test client
        client = TestClient(app)

        # Make request with X-User-ID header
        response = client.get("/test", headers={"X-User-ID": "test-user-123"})

        # Verify response
        assert response.status_code == 200
        assert response.json() == {"user_id": "test-user-123"}
        assert captured_user_id == "test-user-123"

    def test_middleware_handles_missing_header(self):
        """Test that middleware works when X-User-ID header is missing."""
        captured_user_id = None

        # Add a route that captures the user_id from context
        async def test_route(request: Request):
            nonlocal captured_user_id
            captured_user_id = get_current_user_id()
            return JSONResponse({"user_id": captured_user_id})

        # Create a simple Starlette app
        app = Starlette()

        # Add middleware
        app.add_middleware(UserIdExtractionMiddleware)

        # Add route
        app.add_route("/test", test_route, methods=["GET"])

        # Create test client
        client = TestClient(app)

        # Make request without X-User-ID header
        response = client.get("/test")

        # Verify response
        assert response.status_code == 200
        assert response.json() == {"user_id": None}
        assert captured_user_id is None

    def test_middleware_clears_context_after_request(self):
        """Test that middleware clears context variable after request completes."""

        # Add a route
        async def test_route(request: Request):
            return JSONResponse({"status": "ok"})

        # Create a simple Starlette app
        app = Starlette()

        # Add middleware
        app.add_middleware(UserIdExtractionMiddleware)

        # Add route
        app.add_route("/test", test_route, methods=["GET"])

        # Create test client
        client = TestClient(app)

        # Make first request with user_id
        response1 = client.get("/test", headers={"X-User-ID": "user-1"})
        assert response1.status_code == 200

        # Verify context is cleared after first request
        # (context should be None outside of request scope)
        assert get_current_user_id() is None

        # Make second request with different user_id
        response2 = client.get("/test", headers={"X-User-ID": "user-2"})
        assert response2.status_code == 200

        # Verify context is cleared after second request
        assert get_current_user_id() is None

    def test_middleware_isolates_user_id_between_requests(self):
        """Test that middleware properly isolates user_id between concurrent requests."""
        request_results = {}

        # Add a route that captures the user_id
        async def test_route(request: Request):
            request_id = request.path_params["request_id"]
            user_id = get_current_user_id()
            request_results[request_id] = user_id
            return JSONResponse({"request_id": request_id, "user_id": user_id})

        # Create a simple Starlette app
        app = Starlette()

        # Add middleware
        app.add_middleware(UserIdExtractionMiddleware)

        # Add route
        app.add_route("/test/{request_id}", test_route, methods=["GET"])

        # Create test client
        client = TestClient(app)

        # Make multiple requests with different user_ids
        response1 = client.get("/test/req1", headers={"X-User-ID": "alice"})
        response2 = client.get("/test/req2", headers={"X-User-ID": "bob"})
        response3 = client.get("/test/req3", headers={"X-User-ID": "charlie"})

        # Verify each request got the correct user_id
        assert response1.json() == {"request_id": "req1", "user_id": "alice"}
        assert response2.json() == {"request_id": "req2", "user_id": "bob"}
        assert response3.json() == {"request_id": "req3", "user_id": "charlie"}

        assert request_results["req1"] == "alice"
        assert request_results["req2"] == "bob"
        assert request_results["req3"] == "charlie"

    def test_middleware_uses_correct_header_name(self):
        """Test that middleware uses USER_ID_HEADER constant."""
        captured_user_id = None

        # Add a route
        async def test_route(request: Request):
            nonlocal captured_user_id
            captured_user_id = get_current_user_id()
            return JSONResponse({"user_id": captured_user_id})

        # Create a simple Starlette app
        app = Starlette()

        # Add middleware
        app.add_middleware(UserIdExtractionMiddleware)

        # Add route
        app.add_route("/test", test_route, methods=["GET"])

        # Create test client
        client = TestClient(app)

        # Make request with the header name from USER_ID_HEADER constant
        response = client.get("/test", headers={USER_ID_HEADER: "test-user"})

        # Verify it was extracted correctly
        assert response.status_code == 200
        assert response.json() == {"user_id": "test-user"}
        assert captured_user_id == "test-user"
