package main

import (
	"context"
	"io/fs"
	"log"
	"os"
	"strconv"

	e2emocks "github.com/kagent-dev/kagent/go/test/e2e/mocks"
	"github.com/kagent-dev/mockllm"
)

func main() {
	agentServiceAccount := "system:serviceaccount:kagent:test-sts"
	stsPort := 8091
	if port := os.Getenv("STS_PORT"); port != "" {
		stsPort, _ = strconv.Atoi(port)
	}
	stsServer := e2emocks.NewMockSTSServer(agentServiceAccount, uint16(stsPort))
	defer stsServer.Close()

	mockFolder := "./test/e2e/mocks" // assume we are in the go folder, otherwise go run won't work
	if len(os.Args) < 2 {
		log.Fatalf("Usage: %s <mockllm config file>", os.Args[0])
	}
	mockFile := os.Args[1]
	log.Println("mock folder", mockFolder)
	mockllmCfg, err := mockllm.LoadConfigFromFile(mockFile, os.DirFS(mockFolder).(fs.ReadFileFS))
	if err != nil {
		log.Fatalf("Failed to load mockllm config: %v", err)
	}
	mockllmCfg.ListenAddr = ":8090"
	if port := os.Getenv("LLM_PORT"); port != "" {
		mockllmCfg.ListenAddr = ":" + port
	}
	server := mockllm.NewServer(mockllmCfg)
	baseURL, err := server.Start(context.Background())
	if err != nil {
		log.Fatalf("Failed to start mockllm server: %v", err)
	}
	defer server.Stop(context.Background())

	log.Printf("Mock LLM server started at %s", baseURL)
	log.Printf("Mock STS server started at %s", stsServer.URL())
	select {}
}
