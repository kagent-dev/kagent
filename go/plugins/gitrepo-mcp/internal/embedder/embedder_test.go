package embedder

import (
	"math"
	"testing"
)

func TestHashEmbedder_Interface(t *testing.T) {
	var _ EmbeddingModel = (*HashEmbedder)(nil)
}

func TestHashEmbedder_ModelName(t *testing.T) {
	e := NewHashEmbedder(768)
	if e.ModelName() != "hash-embedder" {
		t.Errorf("ModelName() = %q, want %q", e.ModelName(), "hash-embedder")
	}
}

func TestHashEmbedder_Dimensions(t *testing.T) {
	e := NewHashEmbedder(768)
	if e.Dimensions() != 768 {
		t.Errorf("Dimensions() = %d, want %d", e.Dimensions(), 768)
	}
}

func TestHashEmbedder_EmbedBatch(t *testing.T) {
	e := NewHashEmbedder(384)
	texts := []string{"hello world", "func main()", "class Foo"}

	vecs, err := e.EmbedBatch(texts)
	if err != nil {
		t.Fatalf("EmbedBatch() error: %v", err)
	}
	if len(vecs) != 3 {
		t.Fatalf("EmbedBatch() returned %d vectors, want 3", len(vecs))
	}
	for i, vec := range vecs {
		if len(vec) != 384 {
			t.Errorf("vector[%d] length = %d, want 384", i, len(vec))
		}
	}
}

func TestHashEmbedder_Deterministic(t *testing.T) {
	e := NewHashEmbedder(128)
	text := "func Add(a, b int) int { return a + b }"

	v1, _ := e.EmbedBatch([]string{text})
	v2, _ := e.EmbedBatch([]string{text})

	for i := range v1[0] {
		if v1[0][i] != v2[0][i] {
			t.Fatalf("not deterministic at index %d: %f != %f", i, v1[0][i], v2[0][i])
		}
	}
}

func TestHashEmbedder_DifferentInputsDifferentVectors(t *testing.T) {
	e := NewHashEmbedder(128)
	vecs, _ := e.EmbedBatch([]string{"hello", "world"})

	same := true
	for i := range vecs[0] {
		if vecs[0][i] != vecs[1][i] {
			same = false
			break
		}
	}
	if same {
		t.Error("different inputs produced identical vectors")
	}
}

func TestHashEmbedder_UnitVector(t *testing.T) {
	e := NewHashEmbedder(768)
	vecs, _ := e.EmbedBatch([]string{"test normalization"})

	var norm float64
	for _, v := range vecs[0] {
		norm += float64(v) * float64(v)
	}
	norm = math.Sqrt(norm)

	if math.Abs(norm-1.0) > 1e-5 {
		t.Errorf("L2 norm = %f, want ~1.0", norm)
	}
}

func TestHashEmbedder_EmptyBatch(t *testing.T) {
	e := NewHashEmbedder(64)
	vecs, err := e.EmbedBatch(nil)
	if err != nil {
		t.Fatalf("EmbedBatch(nil) error: %v", err)
	}
	if len(vecs) != 0 {
		t.Errorf("EmbedBatch(nil) returned %d vectors, want 0", len(vecs))
	}
}

func TestHashEmbedder_EmptyString(t *testing.T) {
	e := NewHashEmbedder(64)
	vecs, err := e.EmbedBatch([]string{""})
	if err != nil {
		t.Fatalf("EmbedBatch error: %v", err)
	}
	if len(vecs) != 1 || len(vecs[0]) != 64 {
		t.Errorf("unexpected result for empty string")
	}
}
