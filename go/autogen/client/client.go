package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

func New(baseURL string) *Client {
	// Ensure baseURL doesn't end with a slash
	baseURL = strings.TrimRight(baseURL, "/")

	return &Client{
		BaseURL: baseURL,
		HTTPClient: &http.Client{
			Timeout: time.Minute * 30,
		},
	}
}

func (c *Client) GetVersion() (string, error) {
	var result struct {
		Version string `json:"version"`
	}

	err := c.doRequest("GET", "/version", nil, &result)
	if err != nil {
		return "", err
	}

	return result.Version, nil
}

func (c *Client) startRequest(method, path string, body interface{}) (*http.Response, error) {
	var bodyReader *bytes.Reader
	if body != nil {
		bodyBytes, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("error marshaling request body: %w", err)
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}

	// Ensure path starts with a slash
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	url := c.BaseURL + path

	var req *http.Request
	var err error
	if bodyReader != nil {
		req, err = http.NewRequest(method, url, bodyReader)
	} else {
		req, err = http.NewRequest(method, url, nil)
	}
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	return c.HTTPClient.Do(req)
}

func (c *Client) doRequest(method, path string, body interface{}, result interface{}) error {
	resp, err := c.startRequest(method, path, body)
	if err != nil {
		return fmt.Errorf("error making request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("request failed with status: %s", resp.Status)
	}

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("error reading response: %w", err)
	}

	// Try decoding into APIResponse first
	var apiResp APIResponse

	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&apiResp); err != nil {
		// Trying the base value
		return json.Unmarshal(b, result)
	} else {
		// Check response status
		if !apiResp.Status {
			return fmt.Errorf("api error: [%+v]", apiResp)
		}

		// If caller wants the result, marshal the Data field into their result type
		if result != nil {
			dataBytes, err := json.Marshal(apiResp.Data)
			if err != nil {
				return fmt.Errorf("error re-marshaling data: %w", err)
			}

			if err := json.Unmarshal(dataBytes, result); err != nil {
				return fmt.Errorf("error unmarshaling into result: %w", err)
			}
		}
	}

	return nil
}
