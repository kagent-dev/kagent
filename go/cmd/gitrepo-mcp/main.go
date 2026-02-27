package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/kagent-dev/kagent/go/cmd/gitrepo-mcp/internal/config"
	"github.com/kagent-dev/kagent/go/cmd/gitrepo-mcp/internal/embedder"
	"github.com/kagent-dev/kagent/go/cmd/gitrepo-mcp/internal/indexer"
	"github.com/kagent-dev/kagent/go/cmd/gitrepo-mcp/internal/repo"
	"github.com/kagent-dev/kagent/go/cmd/gitrepo-mcp/internal/search"
	"github.com/kagent-dev/kagent/go/cmd/gitrepo-mcp/internal/storage"
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

			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer cancel()

			log.Printf("gitrepo-mcp serve: addr=%s transport=%s data-dir=%s", addr, transport, cfgDataDir)

			_ = mgr
			_ = ctx
			// TODO: wire REST API + MCP server (Steps 7 & 8)
			log.Printf("serve command not yet fully implemented")
			<-ctx.Done()
			return nil
		},
	}

	cmd.Flags().StringVar(&addr, "addr", envOrDefault("GITREPO_ADDR", ":8090"), "listen address")
	cmd.Flags().StringVar(&transport, "transport", envOrDefault("GITREPO_TRANSPORT", "http"), "transport mode: http or stdio")

	return cmd
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
	return &cobra.Command{
		Use:   "sync <name>",
		Short: "Pull latest changes for a repository",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, err := initRepoManager()
			if err != nil {
				return fmt.Errorf("failed to initialize: %w", err)
			}

			name := args[0]
			r, err := mgr.Sync(name)
			if err != nil {
				return err
			}

			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(r)
		},
	}
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
