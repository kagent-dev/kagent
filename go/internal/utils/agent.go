package utils

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kagent-dev/kagent/go/internal/adk"
	"trpc.group/trpc-go/trpc-a2a-go/server"
)

const AGENT_CARD_WELL_KNOWN_PATH = "/.well-known/agent-card.json"

// ComputeConfig unmarshals an agent's config and card and returns them as strings
func ComputeConfig(cfg *adk.AgentConfig, card *server.AgentCard) (string, string, uint64, error) {
	bCfg, err := json.Marshal(cfg)
	if err != nil {
		return "", "", 0, err
	}
	bCard, err := json.Marshal(card)
	if err != nil {
		return "", "", 0, err
	}

	hasher := sha256.New()
	hasher.Write(bCfg)
	hasher.Write(bCard)
	hash := hasher.Sum(nil)
	return string(bCfg), string(bCard), binary.BigEndian.Uint64(hash[:8]), nil
}

// GetAgentCardURL returns the agent card URL for a given discovery URL using its well-known path
func GetAgentCardURL(discoveryURL string) string {
	// remove trailing slash from discoveryURL
	discoveryURL = strings.TrimSuffix(discoveryURL, "/")

	return fmt.Sprintf("%s%s", discoveryURL, AGENT_CARD_WELL_KNOWN_PATH)
}
