package docs

import (
	"context"
	"encoding/binary"
	"fmt"
	"io/ioutil"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/ncruces/go-sqlite3"
	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
)

// Documentation query tools with SQLite Vec support
// Note: This implementation provides basic functionality without external sqlite-vec bindings
// For production use with real vector search, integrate with appropriate embedding services

const (
	DefaultDBURL = "https://doc-sqlite-db.s3.sa-east-1.amazonaws.com"
)

var ProductDBMap = map[string]string{
	"kubernetes":    "kubernetes.db",
	"istio":         "istio.db",
	"argo":          "argo.db",
	"argo-rollouts": "argo-rollouts.db",
	"helm":          "helm.db",
	"prometheus":    "prometheus.db",
	"gateway-api":   "gateway-api.db",
	"gloo-gateway":  "gloo-gateway.db",
	"kgateway":      "kgateway.db",
	"gloo-edge":     "gloo-edge.db",
	"otel":          "otel.db",
}

// DocumentResult represents a search result from the documentation
type DocumentResult struct {
	ID       int     `json:"id"`
	Title    string  `json:"title"`
	Content  string  `json:"content"`
	URL      string  `json:"url"`
	Score    float64 `json:"score"`
	Metadata string  `json:"metadata"`
}

// initializeSQLiteVec initializes sqlite-vec extension (placeholder for future implementation)
func initializeSQLiteVec() error {
	// Placeholder - in production, this would initialize the sqlite-vec extension
	return nil
}

// serializeFloat32 converts a float32 slice to bytes for storage
func serializeFloat32(data []float32) ([]byte, error) {
	buf := make([]byte, len(data)*4)
	for i, f := range data {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf, nil
}

// generateOpenAICompatibleEmbedding creates embeddings using OpenAI API if available
// This function attempts to create embeddings compatible with doc2vec (text-embedding-3-large)
func generateOpenAICompatibleEmbedding(text string) ([]float32, error) {
	// This is a placeholder for OpenAI API integration
	// In production, you would implement actual OpenAI API calls here
	// For now, return an error to fallback to simple embedding
	return nil, fmt.Errorf("OpenAI API integration not implemented")
}

// generateSimpleEmbedding creates a simple embedding vector for text
// In a production system, you would use a proper embedding model like OpenAI's text-embedding-ada-002
func generateSimpleEmbedding(text string) ([]float32, error) {
	// This is a very simplified embedding generation for demonstration
	// In practice, you would use a real embedding model
	words := strings.Fields(strings.ToLower(text))
	if len(words) == 0 {
		return make([]float32, 3072), nil // Return zero vector with 3072 dimensions for doc2vec compatibility
	}

	// Create a simple hash-based embedding (3072 dimensions for doc2vec compatibility)
	embedding := make([]float32, 3072)
	for i, word := range words {
		if i >= 3072 {
			break
		}
		// Simple character-based hash
		hash := float32(0)
		for _, char := range word {
			hash += float32(char)
		}
		embedding[i%3072] = hash / 1000.0 // Normalize
	}

	// Normalize the vector
	var magnitude float32
	for _, val := range embedding {
		magnitude += val * val
	}
	magnitude = float32(math.Sqrt(float64(magnitude)))

	if magnitude > 0 {
		for i := range embedding {
			embedding[i] /= magnitude
		}
	}

	return embedding, nil
}

// searchVectorDatabase performs vector search in the SQLite database with doc2vec compatibility
// Using ncruces/go-sqlite3 with WASM-based sqlite-vec
func searchVectorDatabase(dbPath, query string, limit int) ([]DocumentResult, error) {
	// Open database using ncruces driver with sqlite-vec support
	db, err := sqlite3.Open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}
	defer db.Close()

	// Check if vec_version is available
	stmt, _, err := db.Prepare("SELECT vec_version()")
	if err != nil {
		return nil, fmt.Errorf("sqlite-vec not available: %w", err)
	}

	if stmt.Step() {
		vecVersion := stmt.ColumnText(0)
		stmt.Close()
		fmt.Printf("sqlite-vec version: %s\n", vecVersion)
	} else {
		stmt.Close()
		return nil, fmt.Errorf("failed to get sqlite-vec version")
	}

	// Generate embedding for the query (try to use OpenAI-compatible embedding if possible)
	queryEmbedding, err := generateOpenAICompatibleEmbedding(query)
	if err != nil {
		// Fallback to simple embedding
		queryEmbedding, err = generateSimpleEmbedding(query)
		if err != nil {
			return nil, fmt.Errorf("failed to generate query embedding: %w", err)
		}
	}

	// Check if the doc2vec table exists (vec_items is the standard table name for doc2vec)
	checkStmt, _, err := db.Prepare("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='vec_items'")
	if err != nil {
		return nil, fmt.Errorf("failed to check table existence: %w", err)
	}

	var tableExists bool
	if checkStmt.Step() {
		tableExists = checkStmt.ColumnInt(0) > 0
	}
	checkStmt.Close()

	if !tableExists {
		return nil, fmt.Errorf("doc2vec compatible table 'vec_items' not found in database")
	}

	// Use doc2vec standard query format with MATCH operator
	// The vec_items table uses: embedding MATCH ? for vector search
	sqlQuery := `
		SELECT 
			rowid,
			content,
			url,
			product_name,
			version,
			section,
			distance
		FROM vec_items 
		WHERE embedding MATCH ?
		ORDER BY distance ASC 
		LIMIT ?`

	// Serialize the embedding for sqlite-vec
	serializedEmbedding, err := serializeFloat32(queryEmbedding)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize embedding: %w", err)
	}

	searchStmt, _, err := db.Prepare(sqlQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare search query: %w", err)
	}
	defer searchStmt.Close()

	// Bind parameters
	searchStmt.BindBlob(1, serializedEmbedding)
	searchStmt.BindInt64(2, int64(limit))

	var results []DocumentResult
	for searchStmt.Step() {
		id := searchStmt.ColumnInt(0)
		content := searchStmt.ColumnText(1)
		url := searchStmt.ColumnText(2)
		productName := searchStmt.ColumnText(3)
		version := searchStmt.ColumnText(4)
		section := searchStmt.ColumnText(5)
		distance := searchStmt.ColumnFloat(6)

		// Extract title from section or URL
		title := section
		if title == "" {
			// Try to extract title from URL
			parts := strings.Split(url, "/")
			if len(parts) > 0 {
				title = parts[len(parts)-1]
			}
		}

		// Convert distance to similarity score (0-1, higher is better)
		score := 1.0 - distance

		results = append(results, DocumentResult{
			ID:       id,
			Title:    title,
			Content:  content,
			URL:      url,
			Score:    score,
			Metadata: fmt.Sprintf("Product: %s, Version: %s", productName, version),
		})
	}

	return results, nil
}

func downloadDB(product, dbURL, dbDir string) error {
	dbFile, exists := ProductDBMap[product]
	if !exists {
		return fmt.Errorf("unsupported product: %s", product)
	}

	if dbURL == "" {
		dbURL = DefaultDBURL
	}

	url := fmt.Sprintf("%s/%s", strings.TrimSuffix(dbURL, "/"), dbFile)
	localPath := filepath.Join(dbDir, dbFile)

	// Check if file already exists
	if _, err := os.Stat(localPath); err == nil {
		return nil // File already exists
	}

	// Create directory if it doesn't exist
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Download the file
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("failed to download database: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download database: HTTP %d", resp.StatusCode)
	}

	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	if err := ioutil.WriteFile(localPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

func handleQueryDocumentation(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	product := mcp.ParseString(request, "product", "")
	query := mcp.ParseString(request, "query", "")
	dbURL := mcp.ParseString(request, "db_url", DefaultDBURL)
	dbDir := mcp.ParseString(request, "db_dir", "./docs_db")

	if product == "" {
		return mcp.NewToolResultError("product parameter is required"), nil
	}
	if query == "" {
		return mcp.NewToolResultError("query parameter is required"), nil
	}

	// Check if product is supported
	if _, exists := ProductDBMap[product]; !exists {
		supportedProducts := make([]string, 0, len(ProductDBMap))
		for k := range ProductDBMap {
			supportedProducts = append(supportedProducts, k)
		}
		return mcp.NewToolResultError(fmt.Sprintf("Unsupported product: %s. Supported products: %s",
			product, strings.Join(supportedProducts, ", "))), nil
	}

	// Download database if needed
	if err := downloadDB(product, dbURL, dbDir); err != nil {
		return mcp.NewToolResultError("Failed to download database: " + err.Error()), nil
	}

	// Parse limit
	limitStr := mcp.ParseString(request, "limit", "5")
	limit := 5
	if limitStr != "" {
		var parsedLimit int
		if n, err := fmt.Sscanf(limitStr, "%d", &parsedLimit); n == 1 && err == nil && parsedLimit > 0 {
			limit = parsedLimit
		}
	}

	// Perform vector search
	dbFile := ProductDBMap[product]
	localPath := filepath.Join(dbDir, dbFile)

	// Check if file exists
	if _, err := os.Stat(localPath); err != nil {
		return mcp.NewToolResultError("Database file not found: " + err.Error()), nil
	}

	// Perform vector search
	results, err := searchVectorDatabase(localPath, query, limit)
	if err != nil {
		// Fallback to simple file existence check if vector search fails
		return mcp.NewToolResultText(fmt.Sprintf(`Documentation database loaded for product '%s'.
Database file: %s
Query: "%s"
Error performing vector search: %s

The database is available but vector search failed. This might be because:
1. The database doesn't have vector embeddings
2. The table structure is different than expected
3. sqlite-vec extension is not properly loaded

You can manually inspect the database or use the Python implementation for full functionality.`,
			product, localPath, query, err.Error())), nil
	}

	// Format results
	if len(results) == 0 {
		return mcp.NewToolResultText(fmt.Sprintf("No results found for query '%s' in %s documentation.", query, product)), nil
	}

	var resultText strings.Builder
	resultText.WriteString(fmt.Sprintf("Found %d results for query '%s' in %s documentation:\n\n", len(results), query, product))

	for i, result := range results {
		resultText.WriteString(fmt.Sprintf("Result %d (Score: %.3f):\n", i+1, result.Score))
		if result.Title != "" {
			resultText.WriteString(fmt.Sprintf("Title: %s\n", result.Title))
		}
		if result.URL != "" {
			resultText.WriteString(fmt.Sprintf("URL: %s\n", result.URL))
		}
		if result.Content != "" {
			// Truncate content if too long
			content := result.Content
			if len(content) > 300 {
				content = content[:300] + "..."
			}
			resultText.WriteString(fmt.Sprintf("Content: %s\n", content))
		}
		resultText.WriteString("\n")
	}

	return mcp.NewToolResultText(resultText.String()), nil
}

func handleListSupportedProducts(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	products := make([]string, 0, len(ProductDBMap))
	for product := range ProductDBMap {
		products = append(products, product)
	}

	result := fmt.Sprintf("Supported products for documentation queries:\n%s", strings.Join(products, "\n"))
	return mcp.NewToolResultText(result), nil
}

func RegisterDocsTools(s *server.MCPServer) {
	s.AddTool(mcp.NewTool("query_documentation",
		mcp.WithDescription("Query documentation for supported products using vector search"),
		mcp.WithString("product", mcp.Description("Product to query documentation for"), mcp.Required()),
		mcp.WithString("query", mcp.Description("Query text to search for"), mcp.Required()),
		mcp.WithString("db_url", mcp.Description("Base URL for downloading documentation databases")),
		mcp.WithString("db_dir", mcp.Description("Directory to store documentation databases")),
		mcp.WithString("limit", mcp.Description("Maximum number of results to return (default: 5)")),
	), handleQueryDocumentation)

	s.AddTool(mcp.NewTool("list_supported_products",
		mcp.WithDescription("List supported products for documentation queries"),
	), handleListSupportedProducts)
}
