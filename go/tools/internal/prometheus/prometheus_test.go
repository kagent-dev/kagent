package prometheus

import (
	"net/http"
)

// mockRoundTripper is used to mock HTTP responses for testing
type mockRoundTripper struct {
	response *http.Response
	err      error
}

func (m *mockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.response, nil
}

func newTestClient(response *http.Response, err error) *http.Client {
	return &http.Client{
		Transport: &mockRoundTripper{
			response: response,
			err:      err,
		},
	}
}
