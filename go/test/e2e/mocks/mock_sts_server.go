package mocks

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"
)

// MockSTSServer provides a mock STS server for testing token exchange
type MockSTSServer struct {
	server              *httptest.Server
	requests            []STSTokenRequest
	k8sURL              string
	agentServiceAccount string
}

// STSTokenRequest represents a token exchange request
type STSTokenRequest struct {
	GrantType          string `json:"grant_type"`
	SubjectToken       string `json:"subject_token"`
	SubjectTokenType   string `json:"subject_token_type"`
	ActorToken         string `json:"actor_token,omitempty"`
	ActorTokenType     string `json:"actor_token_type,omitempty"`
	Resource           string `json:"resource,omitempty"`
	Audience           string `json:"audience,omitempty"`
	Scope              string `json:"scope,omitempty"`
	RequestedTokenType string `json:"requested_token_type,omitempty"`
}

// STSTokenResponse represents a token exchange response
type STSTokenResponse struct {
	AccessToken     string `json:"access_token"`
	TokenType       string `json:"token_type"`
	ExpiresIn       int    `json:"expires_in"`
	Scope           string `json:"scope,omitempty"`
	IssuedTokenType string `json:"issued_token_type"`
}

// NewMockSTSServer creates a new mock STS server
func NewMockSTSServer(agentServiceAccount string, port uint16) *MockSTSServer {
	mock := &MockSTSServer{
		requests:            make([]STSTokenRequest, 0),
		agentServiceAccount: agentServiceAccount,
	}

	// Use httptest.NewUnstartedServer to get more control over the server
	mock.server = httptest.NewUnstartedServer(http.HandlerFunc(mock.handleRequest))

	// Configure the server to listen on all interfaces
	mock.server.Listener, _ = net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", port))

	// Start the server
	mock.server.Start()

	return mock
}

// SetK8sURL sets the Kubernetes-accessible URL for the server
func (m *MockSTSServer) SetK8sURL(k8sURL string) {
	m.k8sURL = k8sURL
}

// URL returns the base URL of the mock server
func (m *MockSTSServer) URL() string {
	return m.server.URL
}

// WellKnownURL returns the well-known configuration URL
func (m *MockSTSServer) WellKnownURL() string {
	return m.server.URL + "/.well-known/oauth-authorization-server"
}

// Close stops the mock server
func (m *MockSTSServer) Close() {
	m.server.Close()
}

// GetRequests returns all token exchange requests received
func (m *MockSTSServer) GetRequests() []STSTokenRequest {
	return m.requests
}

// ClearRequests clears the request history
func (m *MockSTSServer) ClearRequests() {
	m.requests = make([]STSTokenRequest, 0)
}

func (m *MockSTSServer) handleRequest(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/.well-known/oauth-authorization-server":
		m.handleWellKnown(w)
	case "/token":
		m.handleTokenExchange(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (m *MockSTSServer) handleWellKnown(w http.ResponseWriter) {
	baseURL := m.server.URL
	if m.k8sURL != "" {
		baseURL = m.k8sURL
	}

	wellKnownConfig := map[string]any{
		"issuer":         baseURL,
		"token_endpoint": baseURL + "/token",
	}

	w.Header().Set("Content-Type", "application/json")
	err := json.NewEncoder(w).Encode(wellKnownConfig)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}

func (m *MockSTSServer) handleTokenExchange(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	contentType := r.Header.Get("Content-Type")
	var req STSTokenRequest

	if strings.Contains(contentType, "application/json") {
		// Parse JSON request
		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Failed to read body", http.StatusBadRequest)
			return
		}

		if err := json.Unmarshal(bodyBytes, &req); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}
	} else {
		// Parse form data request (application/x-www-form-urlencoded)
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Failed to parse form", http.StatusBadRequest)
			return
		}

		// Convert form data to our struct
		req = STSTokenRequest{
			GrantType:          r.FormValue("grant_type"),
			SubjectToken:       r.FormValue("subject_token"),
			SubjectTokenType:   r.FormValue("subject_token_type"),
			ActorToken:         r.FormValue("actor_token"),
			ActorTokenType:     r.FormValue("actor_token_type"),
			Resource:           r.FormValue("resource"),
			Audience:           r.FormValue("audience"),
			Scope:              r.FormValue("scope"),
			RequestedTokenType: r.FormValue("requested_token_type"),
		}
	}

	if req.SubjectToken == "" {
		http.Error(w, "Missing subject_token", http.StatusBadRequest)
		return
	}

	// check if the may act claim is present in the subject token
	// and that it matches the agent service account
	if mayAct, err := extractMayActFromJWT(req.SubjectToken); err != nil {
		http.Error(w, fmt.Sprintf("Error extracting may_act from JWT: %v", err), http.StatusBadRequest)
		return
	} else if mayAct != m.agentServiceAccount {
		http.Error(w, fmt.Sprintf("Invalid may_act claim: %s, expected: %s", mayAct, m.agentServiceAccount), http.StatusBadRequest)
		return
	}

	accessToken, err := m.generateMockAccessToken(req.SubjectToken)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error generating mock access token: %v", err), http.StatusBadRequest)
		return
	}

	response := STSTokenResponse{
		AccessToken:     accessToken,
		TokenType:       "Bearer",
		ExpiresIn:       3600,
		IssuedTokenType: "urn:ietf:params:oauth:token-type:access_token",
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	// store the request so we can verify the sts server received the request in our test
	m.requests = append(m.requests, req)
}

func (m *MockSTSServer) generateMockAccessToken(subjectToken string) (string, error) {
	// Try to parse JWT token to extract subject claim
	subject, err := extractSubjectFromJWT(subjectToken)
	if err != nil {
		return "", fmt.Errorf("failed to extract subject from JWT: %v", err)
	}

	if subject == "" {
		return "", fmt.Errorf("invalid access token subject claim not found")
	}

	tokenData := map[string]any{
		"sub":   subject,
		"scope": "read write",
		"iat":   time.Now().Unix(),
		"exp":   time.Now().Add(time.Hour).Unix(),
		"iss":   "mock-sts-server",
	}

	// For testing purposes, we'll return a simple JSON string
	// In a real implementation, this would be a signed JWT
	tokenBytes, err := json.Marshal(tokenData)
	if err != nil {
		return "", fmt.Errorf("error marshaling token data: %v", err)
	}
	// base64 encode the token to simulate a real token
	encodedToken := base64.StdEncoding.EncodeToString(tokenBytes)
	return encodedToken, nil
}

// extractSubjectFromJWT extracts the subject claim from a JWT token
func extractSubjectFromJWT(jwtToken string) (string, error) {
	claimValue, err := extractClaimFromJWT(jwtToken, "sub")
	if err != nil {
		return "", fmt.Errorf("failed to extract claims from JWT: %v", err)
	}
	return claimValue, nil
}

func extractMayActFromJWT(jwtToken string) (string, error) {
	claimValue, err := extractClaimFromJWT(jwtToken, "may_act.sub")
	if err != nil {
		return "", fmt.Errorf("failed to extract may_act from JWT: %v", err)
	}
	return claimValue, nil
}

func extractClaimFromJWT(jwtToken string, claim string) (string, error) {
	// Split JWT into parts (header.payload.signature)
	parts := strings.Split(jwtToken, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("invalid JWT format")
	}

	// Decode the payload (second part)
	payload := parts[1]

	// Add padding if needed for base64 decoding
	if len(payload)%4 != 0 {
		payload += strings.Repeat("=", 4-len(payload)%4)
	}

	payloadBytes, err := base64.URLEncoding.DecodeString(payload)
	if err != nil {
		return "", fmt.Errorf("failed to decode JWT payload: %v", err)
	}

	// Parse the JSON payload
	var claims map[string]any
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		return "", fmt.Errorf("failed to parse JWT claims: %v", err)
	}
	separatedClaims := strings.Split(claim, ".")
	for i, curClaim := range separatedClaims {
		isLast := i == len(separatedClaims)-1
		if !isLast {
			if nextMap, ok := claims[curClaim].(map[string]any); ok {
				claims = nextMap
			}
		}
	}
	lastClaim := separatedClaims[len(separatedClaims)-1]
	if claimValue, ok := claims[lastClaim].(string); ok {
		return claimValue, nil
	}
	return "", fmt.Errorf("claim %s found in JWT or not a string", claim)
}
