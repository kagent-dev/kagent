package main

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"strconv"

	e2emocks "github.com/kagent-dev/kagent/go/core/test/e2e/mocks"
	"github.com/kagent-dev/kagent/go/pkg/logging"
	"github.com/kagent-dev/mockllm"
)

func main() {
	logger, err := logging.NewFromEnv(os.Stderr)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	ctx := logging.IntoContext(context.Background(), logger)
	agentServiceAccount := "system:serviceaccount:kagent:test-sts"
	stsPort := 8091
	if port := os.Getenv("STS_PORT"); port != "" {
		stsPort, _ = strconv.Atoi(port)
	}
	stsServer := e2emocks.NewMockSTSServer(agentServiceAccount, uint16(stsPort))
	defer stsServer.Close()

	mockFolder := "./test/e2e/mocks" // assume we are in the go folder, otherwise go run won't work
	if len(os.Args) < 2 {
		logger.ErrorContext(ctx, "mock LLM config file is required")
		return
	}
	mockFile := os.Args[1]
	logger.InfoContext(ctx, "loading mock LLM config", "folder", mockFolder)
	mockllmCfg, err := mockllm.LoadConfigFromFile(mockFile, os.DirFS(mockFolder).(fs.ReadFileFS))
	if err != nil {
		logger.ErrorContext(ctx, "failed to load mock LLM config", "error", err)
		return
	}
	mockllmCfg.ListenAddr = ":8090"
	if port := os.Getenv("LLM_PORT"); port != "" {
		mockllmCfg.ListenAddr = ":" + port
	}
	server := mockllm.NewServer(mockllmCfg)
	baseURL, err := server.Start(ctx)
	if err != nil {
		logger.ErrorContext(ctx, "failed to start mock LLM server", "error", err)
		return
	}
	defer server.Stop(ctx)

	logger.InfoContext(ctx, "mock LLM server started", "url", baseURL)
	logger.InfoContext(ctx, "mock STS server started", "url", stsServer.URL())
	select {}
}
