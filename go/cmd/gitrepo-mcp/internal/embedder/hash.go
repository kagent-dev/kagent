package embedder

import (
	"crypto/sha256"
	"encoding/binary"
	"math"
)

// HashEmbedder generates deterministic embeddings from content hashes.
// It produces consistent vectors: same text always yields the same embedding.
// Useful for development, testing, and as a fallback when ONNX is unavailable.
type HashEmbedder struct {
	dims int
}

// NewHashEmbedder creates a HashEmbedder with the given dimensionality.
func NewHashEmbedder(dims int) *HashEmbedder {
	return &HashEmbedder{dims: dims}
}

func (h *HashEmbedder) ModelName() string { return "hash-embedder" }
func (h *HashEmbedder) Dimensions() int   { return h.dims }

// EmbedBatch generates one embedding per input text.
func (h *HashEmbedder) EmbedBatch(texts []string) ([][]float32, error) {
	result := make([][]float32, len(texts))
	for i, text := range texts {
		result[i] = h.embed(text)
	}
	return result, nil
}

// embed generates a deterministic unit vector from text content.
// Uses SHA256 as a seed for a simple PRNG to fill dimensions, then L2-normalizes.
func (h *HashEmbedder) embed(text string) []float32 {
	vec := make([]float32, h.dims)
	seed := sha256.Sum256([]byte(text))

	// Use the 32-byte hash to seed a simple xorshift PRNG
	var state uint64
	state = binary.LittleEndian.Uint64(seed[:8])
	if state == 0 {
		state = 1
	}

	for i := range vec {
		// xorshift64
		state ^= state << 13
		state ^= state >> 7
		state ^= state << 17
		// Map to [-1, 1] range
		vec[i] = float32(int64(state)) / float32(math.MaxInt64)
	}

	// L2-normalize to unit vector
	var norm float32
	for _, v := range vec {
		norm += v * v
	}
	if norm > 0 {
		invNorm := float32(1.0 / math.Sqrt(float64(norm)))
		for i := range vec {
			vec[i] *= invNorm
		}
	}

	return vec
}
