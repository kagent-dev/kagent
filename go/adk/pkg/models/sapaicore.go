package models

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
)

type SAPAICoreConfig struct {
	TransportConfig
	Model         string
	BaseUrl       string
	ResourceGroup string
	AuthUrl       string
	Headers       map[string]string
}

type SAPAICoreModel struct {
	Config SAPAICoreConfig
	Logger logr.Logger

	mu              sync.Mutex
	token           string
	tokenExpiresAt  time.Time
	deploymentURL   string
	deploymentURLAt time.Time
	httpClient      *http.Client
}

func NewSAPAICoreModelWithLogger(config SAPAICoreConfig, logger logr.Logger) (*SAPAICoreModel, error) {
	if config.BaseUrl == "" {
		return nil, fmt.Errorf("SAP AI Core requires base_url")
	}
	if config.ResourceGroup == "" {
		config.ResourceGroup = "default"
	}

	// Build HTTP client with TLS, custom headers, and timeout support
	httpClient, err := BuildHTTPClient(config.TransportConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create SAP AI Core HTTP client: %w", err)
	}

	return &SAPAICoreModel{
		Config:     config,
		Logger:     logger,
		httpClient: httpClient,
	}, nil
}

func (m *SAPAICoreModel) ensureToken(ctx context.Context) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.token != "" && time.Now().Before(m.tokenExpiresAt.Add(-2*time.Minute)) {
		return m.token, nil
	}

	clientID := os.Getenv("SAP_AI_CORE_CLIENT_ID")
	clientSecret := os.Getenv("SAP_AI_CORE_CLIENT_SECRET")
	if m.Config.AuthUrl == "" || clientID == "" || clientSecret == "" {
		return "", fmt.Errorf("SAP AI Core requires auth_url + SAP_AI_CORE_CLIENT_ID/SECRET env vars")
	}

	tokenURL := strings.TrimRight(m.Config.AuthUrl, "/")
	if !strings.HasSuffix(tokenURL, "/oauth/token") {
		tokenURL += "/oauth/token"
	}

	formData := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {clientID},
		"client_secret": {clientSecret},
	}
	req, err := http.NewRequestWithContext(ctx, "POST", tokenURL, strings.NewReader(formData.Encode()))
	if err != nil {
		return "", fmt.Errorf("failed to create OAuth2 token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("OAuth2 token request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", &orchHTTPError{StatusCode: resp.StatusCode, URL: tokenURL}
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", fmt.Errorf("failed to decode OAuth2 token response: %w", err)
	}

	m.token = tokenResp.AccessToken
	if tokenResp.ExpiresIn > 0 {
		m.tokenExpiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	} else {
		m.tokenExpiresAt = time.Now().Add(12 * time.Hour)
	}
	return m.token, nil
}

func (m *SAPAICoreModel) invalidateToken() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.token = ""
	m.tokenExpiresAt = time.Time{}
}

func (m *SAPAICoreModel) resolveDeploymentURL(ctx context.Context) (string, error) {
	m.mu.Lock()
	if m.deploymentURL != "" && time.Now().Before(m.deploymentURLAt.Add(time.Hour)) {
		u := m.deploymentURL
		m.mu.Unlock()
		return u, nil
	}
	m.mu.Unlock()

	token, err := m.ensureToken(ctx)
	if err != nil {
		return "", err
	}

	reqURL := fmt.Sprintf("%s/v2/lm/deployments", m.Config.BaseUrl)
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("AI-Resource-Group", m.Config.ResourceGroup)

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to list deployments: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", &orchHTTPError{StatusCode: resp.StatusCode, URL: reqURL}
	}

	var result struct {
		Resources []struct {
			ID            string `json:"id"`
			ScenarioID    string `json:"scenarioId"`
			Status        string `json:"status"`
			DeploymentURL string `json:"deploymentUrl"`
			CreatedAt     string `json:"createdAt"`
		} `json:"resources"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode deployments: %w", err)
	}

	var best string
	var bestCreated string
	for _, d := range result.Resources {
		if d.ScenarioID == "orchestration" && d.Status == "RUNNING" && d.DeploymentURL != "" {
			if d.CreatedAt > bestCreated {
				best = d.DeploymentURL
				bestCreated = d.CreatedAt
			}
		}
	}
	if best == "" {
		return "", fmt.Errorf("no running orchestration deployment found in SAP AI Core")
	}

	m.mu.Lock()
	m.deploymentURL = best
	m.deploymentURLAt = time.Now()
	m.mu.Unlock()

	m.Logger.Info("Resolved SAP AI Core orchestration deployment", "url", best)
	return best, nil
}

func (m *SAPAICoreModel) invalidateDeploymentURL() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deploymentURL = ""
	m.deploymentURLAt = time.Time{}
}
