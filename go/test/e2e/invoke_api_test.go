package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Invoke API", func() {
	Context("when sent to the server", func() {
		var apiURL string

		BeforeEach(func() {
			// get the API URL from environment
			apiURL = os.Getenv("KAGENT_API_URL")
			if apiURL == "" {
				apiURL = "http://localhost:8001" // default to local testing URL
			}
		})

		It("should successfully handle a synchronous invocation", func() {
			// create the request payload
			payload := map[string]interface{}{
				"message": "Test message from integration test",
				"sync":    true,
				"user_id": "integration-test-user",
			}
			payloadBytes, err := json.Marshal(payload)
			Expect(err).NotTo(HaveOccurred())

			// setup a client with timeout
			client := http.Client{
				Timeout: time.Second * 30,
			}

			// create the request
			req, err := http.NewRequest("POST", fmt.Sprintf("%s/api/agents/1/invoke", apiURL), bytes.NewBuffer(payloadBytes))
			Expect(err).NotTo(HaveOccurred())
			req.Header.Set("Content-Type", "application/json")

			// send the request
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			req = req.WithContext(ctx)

			resp, err := client.Do(req)
			if err != nil {
				Skip("Server not available: " + err.Error())
				return
			}
			defer resp.Body.Close()

			// check response
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			// parse the response
			var responseData map[string]interface{}
			err = json.NewDecoder(resp.Body).Decode(&responseData)
			Expect(err).NotTo(HaveOccurred())

			// verify essential fields exist in the response
			Expect(responseData).To(HaveKey("sessionId"))
			Expect(responseData).To(HaveKey("response"))
			Expect(responseData).To(HaveKey("status"))
			Expect(responseData["status"]).To(Equal("completed"))
		})

		It("should successfully handle an asynchronous invocation", func() {
			// create the request payload
			payload := map[string]interface{}{
				"message": "Test message from integration test",
				"sync":    false,
				"user_id": "integration-test-user",
			}
			payloadBytes, err := json.Marshal(payload)
			Expect(err).NotTo(HaveOccurred())

			// setup a client with timeout
			client := http.Client{
				Timeout: time.Second * 30,
			}

			// create the request
			req, err := http.NewRequest("POST", fmt.Sprintf("%s/api/agents/1/invoke", apiURL), bytes.NewBuffer(payloadBytes))
			Expect(err).NotTo(HaveOccurred())
			req.Header.Set("Content-Type", "application/json")

			// send the request
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			req = req.WithContext(ctx)

			resp, err := client.Do(req)
			if err != nil {
				Skip("Server not available: " + err.Error())
				return
			}
			defer resp.Body.Close()

			// check response
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			// parse the response
			var responseData map[string]interface{}
			err = json.NewDecoder(resp.Body).Decode(&responseData)
			Expect(err).NotTo(HaveOccurred())

			// verify essential fields exist in the response
			Expect(responseData).To(HaveKey("sessionId"))
			Expect(responseData).To(HaveKey("statusUrl"))
			Expect(responseData).To(HaveKey("status"))
			Expect(responseData["status"]).To(Equal("processing"))
		})
	})
}) 
