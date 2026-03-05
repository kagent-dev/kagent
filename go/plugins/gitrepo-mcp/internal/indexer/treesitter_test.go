//go:build cgo

package indexer

import (
	"testing"
)

func TestChunkGoFile(t *testing.T) {
	source := `package main

import "fmt"

func hello() {
	fmt.Println("hello")
}

func add(a, b int) int {
	return a + b
}

type Config struct {
	Name string
	Port int
}
`
	chunks, err := ChunkFile("main.go", []byte(source))
	if err != nil {
		t.Fatalf("ChunkFile failed: %v", err)
	}

	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks (hello, add, Config), got %d", len(chunks))
	}

	assertChunk(t, chunks[0], "function", "hello", 5, 7)
	assertChunk(t, chunks[1], "function", "add", 9, 11)
	assertChunk(t, chunks[2], "type", "Config", 13, 16)
}

func TestChunkGoMethod(t *testing.T) {
	source := `package main

type Server struct {
	port int
}

func (s *Server) Start() error {
	return nil
}

func (s *Server) Stop() {
}
`
	chunks, err := ChunkFile("server.go", []byte(source))
	if err != nil {
		t.Fatalf("ChunkFile failed: %v", err)
	}

	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(chunks))
	}

	assertChunk(t, chunks[0], "type", "Server", 3, 5)
	assertChunk(t, chunks[1], "method", "Start", 7, 9)
	assertChunk(t, chunks[2], "method", "Stop", 11, 12)
}

func TestChunkPythonFile(t *testing.T) {
	source := `class Greeter:
    def __init__(self, name):
        self.name = name

    def greet(self):
        return f"Hello, {self.name}!"

def standalone():
    pass
`
	chunks, err := ChunkFile("app.py", []byte(source))
	if err != nil {
		t.Fatalf("ChunkFile failed: %v", err)
	}

	if len(chunks) < 3 {
		t.Fatalf("expected at least 3 chunks, got %d", len(chunks))
	}

	found := findChunkByName(chunks, "Greeter")
	if found == nil {
		t.Fatal("expected chunk for class Greeter")
	}
	if found.ChunkType != "class" {
		t.Errorf("Greeter chunk type = %q, want %q", found.ChunkType, "class")
	}

	found = findChunkByName(chunks, "standalone")
	if found == nil {
		t.Fatal("expected chunk for function standalone")
	}
	if found.ChunkType != "function" {
		t.Errorf("standalone chunk type = %q, want %q", found.ChunkType, "function")
	}
}

func TestChunkJavaScriptFile(t *testing.T) {
	source := `function greet(name) {
  return "Hello, " + name;
}

class Calculator {
  add(a, b) {
    return a + b;
  }
}
`
	chunks, err := ChunkFile("app.js", []byte(source))
	if err != nil {
		t.Fatalf("ChunkFile failed: %v", err)
	}

	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", len(chunks))
	}

	found := findChunkByName(chunks, "greet")
	if found == nil {
		t.Fatal("expected chunk for function greet")
	}
	if found.ChunkType != "function" {
		t.Errorf("greet chunk type = %q, want %q", found.ChunkType, "function")
	}

	found = findChunkByName(chunks, "Calculator")
	if found == nil {
		t.Fatal("expected chunk for class Calculator")
	}
}

func TestChunkTypeScriptFile(t *testing.T) {
	source := `function fetchData(url: string): Promise<Response> {
  return fetch(url);
}

class ApiClient {
  private baseUrl: string;

  constructor(baseUrl: string) {
    this.baseUrl = baseUrl;
  }
}
`
	chunks, err := ChunkFile("api.ts", []byte(source))
	if err != nil {
		t.Fatalf("ChunkFile failed: %v", err)
	}

	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", len(chunks))
	}

	found := findChunkByName(chunks, "fetchData")
	if found == nil {
		t.Fatal("expected chunk for function fetchData")
	}

	found = findChunkByName(chunks, "ApiClient")
	if found == nil {
		t.Fatal("expected chunk for class ApiClient")
	}
}

func TestChunkJavaFile(t *testing.T) {
	source := `public class Calculator {
    public int add(int a, int b) {
        return a + b;
    }

    public int subtract(int a, int b) {
        return a - b;
    }
}
`
	chunks, err := ChunkFile("Calculator.java", []byte(source))
	if err != nil {
		t.Fatalf("ChunkFile failed: %v", err)
	}

	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", len(chunks))
	}

	found := findChunkByName(chunks, "Calculator")
	if found == nil {
		t.Fatal("expected chunk for class Calculator")
	}
	if found.ChunkType != "class" {
		t.Errorf("Calculator chunk type = %q, want %q", found.ChunkType, "class")
	}

	found = findChunkByName(chunks, "add")
	if found == nil {
		t.Fatal("expected chunk for method add")
	}
	if found.ChunkType != "method" {
		t.Errorf("add chunk type = %q, want %q", found.ChunkType, "method")
	}
}

func TestChunkRustFile(t *testing.T) {
	source := `struct Config {
    name: String,
    port: u16,
}

impl Config {
    fn new(name: String, port: u16) -> Self {
        Config { name, port }
    }
}

fn main() {
    let cfg = Config::new("test".to_string(), 8080);
}
`
	chunks, err := ChunkFile("main.rs", []byte(source))
	if err != nil {
		t.Fatalf("ChunkFile failed: %v", err)
	}

	if len(chunks) < 3 {
		t.Fatalf("expected at least 3 chunks, got %d", len(chunks))
	}

	found := findChunkByName(chunks, "Config")
	if found == nil {
		t.Fatal("expected chunk for struct Config")
	}

	found = findChunkByName(chunks, "main")
	if found == nil {
		t.Fatal("expected chunk for function main")
	}
}

func TestChunkGoLineNumbers(t *testing.T) {
	source := `package main

func first() {
	// line 4
}

func second() {
	// line 8
	// line 9
}
`
	chunks, err := ChunkFile("lines.go", []byte(source))
	if err != nil {
		t.Fatalf("ChunkFile failed: %v", err)
	}

	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}

	if chunks[0].LineStart != 3 {
		t.Errorf("first() LineStart = %d, want 3", chunks[0].LineStart)
	}
	if chunks[0].LineEnd != 5 {
		t.Errorf("first() LineEnd = %d, want 5", chunks[0].LineEnd)
	}

	if chunks[1].LineStart != 7 {
		t.Errorf("second() LineStart = %d, want 7", chunks[1].LineStart)
	}
	if chunks[1].LineEnd != 10 {
		t.Errorf("second() LineEnd = %d, want 10", chunks[1].LineEnd)
	}
}

// assertChunk is a helper for tree-sitter chunk assertions.
func assertChunk(t *testing.T, c Chunk, wantType, wantName string, wantStart, wantEnd int) {
	t.Helper()
	if c.ChunkType != wantType {
		t.Errorf("chunk %q type = %q, want %q", c.ChunkName, c.ChunkType, wantType)
	}
	if c.ChunkName != wantName {
		t.Errorf("chunk name = %q, want %q", c.ChunkName, wantName)
	}
	if c.LineStart != wantStart {
		t.Errorf("chunk %q LineStart = %d, want %d", c.ChunkName, c.LineStart, wantStart)
	}
	if c.LineEnd != wantEnd {
		t.Errorf("chunk %q LineEnd = %d, want %d", c.ChunkName, c.LineEnd, wantEnd)
	}
}
