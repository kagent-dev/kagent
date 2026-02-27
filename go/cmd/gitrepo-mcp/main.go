package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/kagent-dev/kagent/go/cmd/gitrepo-mcp/internal/config"
	"github.com/kagent-dev/kagent/go/cmd/gitrepo-mcp/internal/embedder"
	"github.com/kagent-dev/kagent/go/cmd/gitrepo-mcp/internal/indexer"
	"github.com/kagent-dev/kagent/go/cmd/gitrepo-mcp/internal/repo"
	"github.com/kagent-dev/kagent/go/cmd/gitrepo-mcp/internal/search"
	"github.com/kagent-dev/kagent/go/cmd/gitrepo-mcp/internal/server"
	"github.com/kagent-dev/kagent/go/cmd/gitrepo-mcp/internal/storage"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
)

var (
	cfgDataDir string
	cfgDBPath  string
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "gitrepo-mcp",
		Short: "Git repository semantic search and structural search MCP server",
		Long:  "A standalone MCP server that clones git repos, indexes them with local CPU embeddings, and exposes semantic search + ast-grep structural search.",
	}

	rootCmd.PersistentFlags().StringVar(&cfgDataDir, "data-dir", envOrDefault("GITREPO_DATA_DIR", "./data"), "data directory for cloned repos and database")
	rootCmd.PersistentFlags().StringVar(&cfgDBPath, "db-path", envOrDefault("GITREPO_DB_PATH", ""), "SQLite database file path (default: <data-dir>/gitrepo.db)")

	rootCmd.AddCommand(
		newServeCmd(),
		newAddCmd(),
		newListCmd(),
		newRemoveCmd(),
		newSyncCmd(),
		newSyncAllCmd(),
		newIndexCmd(),
		newSearchCmd(),
		newAstSearchCmd(),
	)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getDBPath() string {
	if cfgDBPath != "" {
		return cfgDBPath
	}
	return cfgDataDir + "/gitrepo.db"
}

func initStorage() (*storage.Manager, error) {
	cfg := &config.Config{
		DBType:  config.DBTypeSQLite,
		DBPath:  getDBPath(),
		DataDir: cfgDataDir,
	}
	mgr, err := storage.NewManager(cfg)
	if err != nil {
		return nil, err
	}
	if err := mgr.Initialize(); err != nil {
		return nil, err
	}
	return mgr, nil
}

func initRepoManager() (*repo.Manager, error) {
	dbMgr, err := initStorage()
	if err != nil {
		return nil, err
	}
	repoStore := storage.NewRepoStore(dbMgr.DB())
	reposDir := filepath.Join(cfgDataDir, "repos")
	return repo.NewManager(repoStore, reposDir), nil
}

func newServeCmd() *cobra.Command {
	var addr, transport string

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the REST API and MCP server",
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, err := initStorage()
			if err != nil {
				return fmt.Errorf("failed to initialize storage: %w", err)
			}

			repoStore := storage.NewRepoStore(mgr.DB())
			embStore := storage.NewEmbeddingStore(mgr.DB())
			emb := embedder.NewHashEmbedder(768)

			reposDir := filepath.Join(cfgDataDir, "repos")
			repoMgr := repo.NewManager(repoStore, reposDir)
			idx := indexer.NewIndexer(repoStore, embStore, emb)
			s := search.NewSearcher(repoStore, embStore, emb)
			astS := search.NewAstSearcher(repoStore)

			mcpSrv := server.NewMCPServer(repoStore, repoMgr, idx, s, astS, reposDir)

			if transport == "stdio" {
				return serveStdio(cmd.Context(), mcpSrv)
			}

			return serveHTTP(addr, repoStore, repoMgr, idx, s, astS, reposDir, mcpSrv)
		},
	}

	cmd.Flags().StringVar(&addr, "addr", envOrDefault("GITREPO_ADDR", ":8090"), "listen address")
	cmd.Flags().StringVar(&transport, "transport", envOrDefault("GITREPO_TRANSPORT", "http"), "transport mode: http or stdio")

	return cmd
}

func serveHTTP(addr string, repoStore *storage.RepoStore, repoMgr *repo.Manager, idx *indexer.Indexer, s *search.Searcher, astS *search.AstSearcher, reposDir string, mcpSrv *server.MCPServer) error {
	restSrv := server.NewServer(repoStore, repoMgr, idx, s, astS, reposDir)

	mux := http.NewServeMux()
	mux.Handle("/mcp/", http.StripPrefix("/mcp", mcpSrv))
	mux.Handle("/", restSrv.Handler())

	httpSrv := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	go func() {
		<-ctx.Done()
		log.Printf("shutting down server...")
		_ = httpSrv.Close()
	}()

	log.Printf("gitrepo-mcp serve: addr=%s transport=http data-dir=%s", addr, cfgDataDir)
	log.Printf("  REST API: http://localhost%s/api/", addr)
	log.Printf("  MCP:      http://localhost%s/mcp/", addr)
	if err := httpSrv.ListenAndServe(); err != http.ErrServerClosed {
		return fmt.Errorf("server error: %w", err)
	}
	return nil
}

func serveStdio(ctx context.Context, mcpSrv *server.MCPServer) error {
	log.Printf("gitrepo-mcp serve: transport=stdio data-dir=%s", cfgDataDir)

	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer cancel()

	return mcpSrv.Server().Run(ctx, &mcpsdk.StdioTransport{})
}

func newAddCmd() *cobra.Command {
	var url, branch string

	cmd := &cobra.Command{
		Use:   "add <name>",
		Short: "Register and clone a git repository",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, err := initRepoManager()
			if err != nil {
				return fmt.Errorf("failed to initialize: %w", err)
			}

			name := args[0]
			r, err := mgr.Add(name, url, branch)
			if err != nil {
				return err
			}

			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(r)
		},
	}

	cmd.Flags().StringVar(&url, "url", "", "git repository URL")
	cmd.Flags().StringVar(&branch, "branch", "main", "git branch")
	_ = cmd.MarkFlagRequired("url")

	return cmd
}

func newListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List registered repositories",
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, err := initRepoManager()
			if err != nil {
				return fmt.Errorf("failed to initialize: %w", err)
			}

			repos, err := mgr.List()
			if err != nil {
				return fmt.Errorf("failed to list repos: %w", err)
			}

			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(repos)
		},
	}
}

func newRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a repository and its embeddings",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, err := initRepoManager()
			if err != nil {
				return fmt.Errorf("failed to initialize: %w", err)
			}

			name := args[0]
			if err := mgr.Remove(name); err != nil {
				return err
			}

			log.Printf("removed repo %s", name)
			return nil
		},
	}
}

func newSyncCmd() *cobra.Command {
	var reindex bool

	cmd := &cobra.Command{
		Use:   "sync <name>",
		Short: "Pull latest changes for a repository",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			if reindex {
				dbMgr, err := initStorage()
				if err != nil {
					return fmt.Errorf("failed to initialize: %w", err)
				}
				repoStore := storage.NewRepoStore(dbMgr.DB())
				embStore := storage.NewEmbeddingStore(dbMgr.DB())
				emb := embedder.NewHashEmbedder(768)
				reposDir := filepath.Join(cfgDataDir, "repos")
				mgr := repo.NewManager(repoStore, reposDir)
				idx := indexer.NewIndexer(repoStore, embStore, emb)

				r, reindexed, err := mgr.SyncAndReindex(name, func(n string) error {
					log.Printf("re-indexing repo %s ...", n)
					return idx.Index(n)
				})
				if err != nil {
					return err
				}
				if reindexed {
					log.Printf("repo %s synced and re-indexed", name)
				}

				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(r)
			}

			mgr, err := initRepoManager()
			if err != nil {
				return fmt.Errorf("failed to initialize: %w", err)
			}

			r, err := mgr.Sync(name)
			if err != nil {
				return err
			}

			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(r)
		},
	}

	cmd.Flags().BoolVar(&reindex, "reindex", false, "re-index the repo if it was previously indexed")

	return cmd
}

func newSyncAllCmd() *cobra.Command {
	var reindex bool

	cmd := &cobra.Command{
		Use:   "sync-all",
		Short: "Sync all repositories with optional re-indexing",
		RunE: func(cmd *cobra.Command, args []string) error {
			dbMgr, err := initStorage()
			if err != nil {
				return fmt.Errorf("failed to initialize storage: %w", err)
			}

			repoStore := storage.NewRepoStore(dbMgr.DB())
			reposDir := filepath.Join(cfgDataDir, "repos")
			mgr := repo.NewManager(repoStore, reposDir)

			var reindexFn func(string) error
			if reindex {
				embStore := storage.NewEmbeddingStore(dbMgr.DB())
				emb := embedder.NewHashEmbedder(768)
				idx := indexer.NewIndexer(repoStore, embStore, emb)
				reindexFn = func(name string) error {
					log.Printf("re-indexing repo %s ...", name)
					return idx.Index(name)
				}
			}

			results, err := mgr.SyncAll(reindexFn)
			if err != nil {
				return err
			}

			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(results)
		},
	}

	cmd.Flags().BoolVar(&reindex, "reindex", true, "re-index repos that were previously indexed")

	return cmd
}

func newIndexCmd() *cobra.Command {
	var batchSize int

	cmd := &cobra.Command{
		Use:   "index <name>",
		Short: "Index a repository for semantic search",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dbMgr, err := initStorage()
			if err != nil {
				return fmt.Errorf("failed to initialize storage: %w", err)
			}

			repoStore := storage.NewRepoStore(dbMgr.DB())
			embStore := storage.NewEmbeddingStore(dbMgr.DB())
			emb := embedder.NewHashEmbedder(768)

			idx := indexer.NewIndexer(repoStore, embStore, emb)
			if batchSize > 0 {
				idx.SetBatchSize(batchSize)
			}

			name := args[0]
			log.Printf("indexing repo %s ...", name)
			if err := idx.Index(name); err != nil {
				return err
			}

			r, err := repoStore.Get(name)
			if err != nil {
				return err
			}

			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(r)
		},
	}

	cmd.Flags().IntVar(&batchSize, "batch-size", 32, "embedding batch size")

	return cmd
}

func newSearchCmd() *cobra.Command {
	var query string
	var limit int
	var contextLines int

	cmd := &cobra.Command{
		Use:   "search <name>",
		Short: "Semantic search within a repository",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dbMgr, err := initStorage()
			if err != nil {
				return fmt.Errorf("failed to initialize storage: %w", err)
			}

			repoStore := storage.NewRepoStore(dbMgr.DB())
			embStore := storage.NewEmbeddingStore(dbMgr.DB())
			emb := embedder.NewHashEmbedder(768)

			s := search.NewSearcher(repoStore, embStore, emb)

			name := args[0]
			results, err := s.Search(query, name, limit, contextLines)
			if err != nil {
				return err
			}

			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(results)
		},
	}

	cmd.Flags().StringVarP(&query, "query", "c", "", "search query")
	cmd.Flags().IntVar(&limit, "limit", 10, "maximum number of results")
	cmd.Flags().IntVar(&contextLines, "context", 0, "number of context lines before and after each result")
	_ = cmd.MarkFlagRequired("query")

	return cmd
}

func newAstSearchCmd() *cobra.Command {
	var pattern, lang string

	cmd := &cobra.Command{
		Use:   "ast-search <name>",
		Short: "Structural code search using ast-grep",
		Long:  "Search for code patterns using ast-grep structural matching (e.g., 'func $NAME($$$) error').",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dbMgr, err := initStorage()
			if err != nil {
				return fmt.Errorf("failed to initialize storage: %w", err)
			}

			repoStore := storage.NewRepoStore(dbMgr.DB())
			s := search.NewAstSearcher(repoStore)

			name := args[0]
			results, err := s.Search(pattern, name, lang)
			if err != nil {
				return err
			}

			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(results)
		},
	}

	cmd.Flags().StringVar(&pattern, "pattern", "", "ast-grep pattern (e.g., 'func $NAME($$$) error')")
	cmd.Flags().StringVar(&lang, "lang", "", "language filter (e.g., go, python, javascript)")
	_ = cmd.MarkFlagRequired("pattern")

	return cmd
}
