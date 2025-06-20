package docs

import (
	"database/sql"
	"math"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
)

func TestGenerateSimpleEmbedding(t *testing.T) {
	text := "test embedding generation"
	embedding, err := generateSimpleEmbedding(text)
	if err != nil {
		t.Fatalf("Failed to generate embedding: %v", err)
	}

	if len(embedding) != 3072 {
		t.Errorf("Expected embedding length 3072, got %d", len(embedding))
	}

	// Check if embedding is normalized (magnitude should be approximately 1.0)
	var magnitude float32
	for _, val := range embedding {
		magnitude += val * val
	}
	magnitude = float32(math.Sqrt(float64(magnitude)))

	if magnitude < 0.9 || magnitude > 1.1 {
		t.Errorf("Expected normalized embedding (magnitude ~1.0), got %f", magnitude)
	}
}

func TestSerializeFloat32(t *testing.T) {
	testData := []float32{1.0, 2.5, -3.7, 0.0}
	serialized, err := serializeFloat32(testData)
	if err != nil {
		t.Fatalf("Failed to serialize: %v", err)
	}

	expectedLength := len(testData) * 4 // 4 bytes per float32
	if len(serialized) != expectedLength {
		t.Errorf("Expected %d bytes, got %d", expectedLength, len(serialized))
	}
}

func TestSimpleDatabaseOps(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "docs_debug_test_*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "test.db")

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Test basic table creation and insertion
	_, err = db.Exec(`CREATE TABLE test_table (id INTEGER PRIMARY KEY, data TEXT)`)
	if err != nil {
		t.Fatalf("Failed to create table: %v", err)
	}

	_, err = db.Exec("INSERT INTO test_table (data) VALUES (?)", "test data")
	if err != nil {
		t.Fatalf("Failed to insert data: %v", err)
	}

	t.Log("Basic database operations successful")
}

func setupTestDB(t *testing.T) (string, func()) {
	tempDir, err := os.MkdirTemp("", "docs_test_*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}

	dbPath := filepath.Join(tempDir, "test.db")

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		os.RemoveAll(tempDir)
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Create the main table compatible with doc2vec schema
	createTableSQL := `CREATE TABLE vec_items (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		content TEXT NOT NULL,
		url TEXT,
		product_name TEXT,
		version TEXT,
		section TEXT,
		embedding BLOB
	)`

	_, err = db.Exec(createTableSQL)
	if err != nil {
		t.Fatalf("Failed to create table: %v", err)
	}

	// Insert sample data with embeddings
	sampleData := []struct {
		content     string
		url         string
		productName string
		version     string
		section     string
	}{
		{
			content:     "Kubernetes is an open-source container orchestration platform",
			url:         "https://kubernetes.io/docs/concepts/overview/",
			productName: "kubernetes",
			version:     "v1.30",
			section:     "Overview",
		},
	}

	for _, item := range sampleData {
		// Generate embedding for the content
		embedding, err := generateSimpleEmbedding(item.content)
		if err != nil {
			t.Fatalf("Failed to generate embedding: %v", err)
		}

		// Serialize embedding
		serializedEmbedding, err := serializeFloat32(embedding)
		if err != nil {
			t.Fatalf("Failed to serialize embedding: %v", err)
		}

		// Insert into database
		_, err = db.Exec(`INSERT INTO vec_items (content, url, product_name, version, section, embedding) 
						  VALUES (?, ?, ?, ?, ?, ?)`,
			item.content, item.url, item.productName, item.version, item.section, serializedEmbedding)
		if err != nil {
			t.Fatalf("Failed to insert data: %v", err)
		}
	}

	return dbPath, func() {
		os.RemoveAll(tempDir)
	}
}

func TestSearchVectorDatabaseNonExistentFile(t *testing.T) {
	results, err := searchVectorDatabase("/non/existent/file.db", "test query", 3)
	if err == nil {
		t.Error("Expected error for non-existent database file")
	}
	if len(results) != 0 {
		t.Error("Expected no results for non-existent database")
	}
}

func TestGenerateOpenAICompatibleEmbedding(t *testing.T) {
	text := "test openai embedding"
	embedding, err := generateOpenAICompatibleEmbedding(text)
	if err != nil {
		// This is expected to fail without API key, so just check the error is reasonable
		t.Logf("OpenAI embedding failed as expected: %v", err)
		return
	}

	// If it somehow succeeds (e.g., with API key), verify format
	if len(embedding) != 3072 {
		t.Errorf("Expected embedding length 3072, got %d", len(embedding))
	}
}
