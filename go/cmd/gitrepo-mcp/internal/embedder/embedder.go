package embedder

// EmbeddingModel generates vector embeddings for text.
type EmbeddingModel interface {
	// EmbedBatch embeds a batch of texts and returns one vector per text.
	EmbedBatch(texts []string) ([][]float32, error)

	// Dimensions returns the dimensionality of the embedding vectors.
	Dimensions() int

	// ModelName returns a human-readable model identifier.
	ModelName() string
}
