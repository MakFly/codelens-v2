package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/yourusername/codelens/internal/embeddings"
	"github.com/yourusername/codelens/internal/indexer"
	"github.com/yourusername/codelens/internal/mcp"
	"github.com/yourusername/codelens/internal/store"
	"github.com/yourusername/codelens/internal/watcher"
)

var rootCmd = &cobra.Command{
	Use:   "codelens",
	Short: "Agentic memory & semantic search MCP server for Claude Code",
}

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the MCP server (stdio transport)",
	RunE:  runServe,
}

var indexCmd = &cobra.Command{
	Use:   "index [path]",
	Short: "Index a project directory",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runIndex,
}

var searchCmd = &cobra.Command{
	Use:   "search [query]",
	Short: "Search the codebase (CLI test)",
	Args:  cobra.ExactArgs(1),
	RunE:  runSearch,
}

var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show index statistics",
	RunE:  runStats,
}

var watcherCmd = &cobra.Command{
	Use:   "watcher",
	Short: "Manage background index watcher",
	RunE:  runWatcherAuto,
}

var watcherStartCmd = &cobra.Command{
	Use:   "start [path]",
	Short: "Start watcher daemon in background",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runWatcherStart,
}

var watcherStopCmd = &cobra.Command{
	Use:   "stop [path]",
	Short: "Stop watcher daemon",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runWatcherStop,
}

var watcherStatusCmd = &cobra.Command{
	Use:   "status [path]",
	Short: "Show watcher daemon status",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runWatcherStatus,
}

var watcherRunCmd = &cobra.Command{
	Use:    "run [path]",
	Short:  "Run watcher loop in foreground (internal)",
	Args:   cobra.MaximumNArgs(1),
	RunE:   runWatcherRun,
	Hidden: true,
}

func init() {
	// Flags globaux
	rootCmd.PersistentFlags().String("project", ".", "Project root directory")
	rootCmd.PersistentFlags().String("db", ".codelens/index.db", "SQLite database path (relative to project)")
	rootCmd.PersistentFlags().String("ollama-url", "http://localhost:11434", "Ollama API URL")
	rootCmd.PersistentFlags().String("ollama-model", "nomic-embed-text", "Ollama embedding model")

	viper.BindPFlags(rootCmd.PersistentFlags())
	viper.AutomaticEnv()
	viper.SetEnvPrefix("CODELENS")

	// Flags spécifiques
	indexCmd.Flags().Bool("watch", false, "Watch for file changes after initial index")
	indexCmd.Flags().Bool("force", false, "Re-index all files even if hash unchanged")
	watcherCmd.PersistentFlags().Duration("interval", 5*time.Second, "Watcher reindex interval")
	watcherCmd.PersistentFlags().Bool("force", false, "Force full index at watcher startup")
	watcherCmd.PersistentFlags().String("pid-file", ".codelens/watcher.pid", "Watcher PID file")
	watcherCmd.PersistentFlags().String("state-file", ".codelens/watcher.state.json", "Watcher status state file")
	watcherCmd.PersistentFlags().String("log-file", ".codelens/watcher.log", "Watcher log file")

	watcherCmd.AddCommand(watcherStartCmd, watcherStopCmd, watcherStatusCmd, watcherRunCmd)
	rootCmd.AddCommand(serveCmd, indexCmd, searchCmd, statsCmd, watcherCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runServe(cmd *cobra.Command, args []string) error {
	cfg := loadConfig()

	db, err := store.Open(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	embedder := embeddings.NewOllama(cfg.OllamaURL, cfg.OllamaModel)

	idx, err := indexer.New(cfg.ProjectPath, db, embedder)
	if err != nil {
		return fmt.Errorf("create indexer: %w", err)
	}

	srv := mcp.NewServer(db, idx, embedder)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	return srv.ServeStdio(ctx)
}

func runIndex(cmd *cobra.Command, args []string) error {
	path := "."
	if len(args) > 0 {
		path = args[0]
	}

	cfg := loadConfig()
	watch, _ := cmd.Flags().GetBool("watch")
	force, _ := cmd.Flags().GetBool("force")

	db, err := store.Open(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	embedder := embeddings.NewOllama(cfg.OllamaURL, cfg.OllamaModel)

	idx, err := indexer.New(path, db, embedder)
	if err != nil {
		return fmt.Errorf("create indexer: %w", err)
	}

	fmt.Printf("Indexing %s...\n", path)
	stats, err := idx.IndexAll(context.Background(), force)
	if err != nil {
		return fmt.Errorf("index: %w", err)
	}
	fmt.Printf("✓ Indexed %d files, %d chunks (%d failed files) in %s\n", stats.Files, stats.Chunks, stats.FailedFiles, stats.Duration)

	if watch {
		fmt.Println("Watching for changes (Ctrl+C to stop)...")
		ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer cancel()
		return idx.Watch(ctx)
	}

	return nil
}

func runSearch(cmd *cobra.Command, args []string) error {
	cfg := loadConfig()

	db, err := store.Open(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	embedder := embeddings.NewOllama(cfg.OllamaURL, cfg.OllamaModel)

	idx, err := indexer.New(cfg.ProjectPath, db, embedder)
	if err != nil {
		return fmt.Errorf("create indexer: %w", err)
	}

	results, err := idx.Search(context.Background(), args[0], 5)
	if err != nil {
		return err
	}

	for i, r := range results {
		fmt.Printf("\n[%d] %s:%d-%d (score: %.3f)\n", i+1, r.FilePath, r.StartLine, r.EndLine, r.Score)
		if r.Symbol != "" {
			fmt.Printf("    Symbol: %s (%s)\n", r.Symbol, r.SymbolKind)
		}
		fmt.Printf("    %s\n", truncate(r.Content, 200))
	}

	return nil
}

func runStats(cmd *cobra.Command, args []string) error {
	cfg := loadConfig()

	db, err := store.Open(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	stats, err := db.Stats()
	if err != nil {
		return err
	}

	fmt.Printf("CodeLens Index Stats\n")
	fmt.Printf("====================\n")
	fmt.Printf("Project:    %s\n", cfg.ProjectPath)
	fmt.Printf("DB:         %s\n", cfg.DBPath)
	fmt.Printf("Files:      %d\n", stats.Files)
	fmt.Printf("Chunks:     %d\n", stats.Chunks)
	fmt.Printf("Failed:     %d\n", stats.FailedFiles)
	fmt.Printf("Memories:   %d (active)\n", stats.ActiveMemories)
	fmt.Printf("Last index: %s\n", stats.LastIndexed.Format("2006-01-02 15:04:05"))
	return nil
}

func runWatcherStart(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return runWatcherAuto(cmd, args)
	}
	cfg, err := buildWatcherConfig(cmd, args)
	if err != nil {
		return err
	}
	pid, err := watcher.Start(cfg)
	if err != nil {
		return err
	}
	fmt.Printf("✓ Watcher started (pid: %d)\n", pid)
	fmt.Printf("State: %s\n", cfg.StateFile)
	fmt.Printf("Logs:  %s\n", cfg.LogFile)
	return nil
}

func runWatcherStop(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		projects, err := discoverIndexedProjects()
		if err != nil {
			return err
		}
		if len(projects) == 0 {
			fmt.Println("No indexed projects detected.")
			return nil
		}
		stopped := 0
		for _, p := range projects {
			cfg, cfgErr := buildWatcherConfigFromProject(cmd, p)
			if cfgErr != nil {
				fmt.Printf("✗ %s: %v\n", p, cfgErr)
				continue
			}
			if err := watcher.Stop(cfg, 4*time.Second); err != nil {
				fmt.Printf("✗ %s: %v\n", p, err)
				continue
			}
			fmt.Printf("✓ Stopped: %s\n", p)
			stopped++
		}
		fmt.Printf("Stopped %d/%d watcher(s)\n", stopped, len(projects))
		return nil
	}
	cfg, err := buildWatcherConfig(cmd, args)
	if err != nil {
		return err
	}
	if err := watcher.Stop(cfg, 4*time.Second); err != nil {
		return err
	}
	fmt.Println("✓ Watcher stopped")
	return nil
}

func runWatcherStatus(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		projects, err := discoverIndexedProjects()
		if err != nil {
			return err
		}
		if len(projects) == 0 {
			fmt.Println("No indexed projects detected.")
			return nil
		}
		for i, p := range projects {
			cfg, cfgErr := buildWatcherConfigFromProject(cmd, p)
			if cfgErr != nil {
				fmt.Printf("✗ %s: %v\n", p, cfgErr)
				continue
			}
			status, stErr := watcher.GetStatus(cfg)
			if stErr != nil {
				fmt.Printf("✗ %s: %v\n", p, stErr)
				continue
			}
			out, _ := json.Marshal(status.State)
			fmt.Printf("%s\n", out)
			if i != len(projects)-1 {
				fmt.Println()
			}
		}
		return nil
	}
	cfg, err := buildWatcherConfig(cmd, args)
	if err != nil {
		return err
	}
	status, err := watcher.GetStatus(cfg)
	if err != nil {
		return err
	}
	out, err := json.MarshalIndent(status.State, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(out))
	if status.Stale {
		fmt.Printf("warning: %s\n", status.Reason)
	}
	return nil
}

func runWatcherRun(cmd *cobra.Command, args []string) error {
	cfg, err := buildWatcherConfig(cmd, args)
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	err = watcher.Run(ctx, cfg, runWatcherCycle)
	if err == context.Canceled {
		return nil
	}
	return err
}

func runWatcherCycle(ctx context.Context, cfg watcher.Config) (int, int, error) {
	db, err := store.Open(cfg.DBPath)
	if err != nil {
		return 0, 0, fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	embedder := embeddings.NewOllama(cfg.OllamaURL, cfg.OllamaModel)
	idx, err := indexer.New(cfg.ProjectPath, db, embedder)
	if err != nil {
		return 0, 0, fmt.Errorf("create indexer: %w", err)
	}
	stats, err := idx.IndexAll(ctx, cfg.Force)
	if err != nil {
		return 0, 0, fmt.Errorf("index cycle: %w", err)
	}
	return stats.Files, stats.Chunks, nil
}

func runWatcherAuto(cmd *cobra.Command, args []string) error {
	projects, err := discoverIndexedProjects()
	if err != nil {
		return err
	}
	if len(projects) == 0 {
		fmt.Println("No indexed projects detected. Run `codelens index /path/to/project` first.")
		return nil
	}
	started := 0
	for _, p := range projects {
		cfg, cfgErr := buildWatcherConfigFromProject(cmd, p)
		if cfgErr != nil {
			fmt.Printf("✗ %s: %v\n", p, cfgErr)
			continue
		}
		pid, startErr := watcher.Start(cfg)
		if startErr != nil {
			if strings.Contains(startErr.Error(), "already running") {
				fmt.Printf("• Already running: %s\n", p)
				continue
			}
			fmt.Printf("✗ %s: %v\n", p, startErr)
			continue
		}
		fmt.Printf("✓ Started %s (pid: %d)\n", p, pid)
		started++
	}
	fmt.Printf("Started %d watcher(s), scanned %d indexed project(s)\n", started, len(projects))
	return nil
}

func buildWatcherConfig(cmd *cobra.Command, args []string) (watcher.Config, error) {
	cfg := loadConfig()
	projectPath := cfg.ProjectPath
	if len(args) > 0 && args[0] != "" {
		projectPath = args[0]
	}
	absProject, err := filepath.Abs(projectPath)
	if err != nil {
		return watcher.Config{}, err
	}

	interval, _ := cmd.Flags().GetDuration("interval")
	force, _ := cmd.Flags().GetBool("force")
	pidFile, _ := cmd.Flags().GetString("pid-file")
	stateFile, _ := cmd.Flags().GetString("state-file")
	logFile, _ := cmd.Flags().GetString("log-file")

	resolve := func(path string) string {
		if filepath.IsAbs(path) {
			return path
		}
		return filepath.Join(absProject, path)
	}

	dbPath := cfg.DBPath
	if !filepath.IsAbs(dbPath) {
		dbPath = filepath.Join(absProject, dbPath)
	}

	return watcher.Config{
		ProjectPath: absProject,
		DBPath:      dbPath,
		OllamaURL:   cfg.OllamaURL,
		OllamaModel: cfg.OllamaModel,
		Interval:    interval,
		Force:       force,
		PIDFile:     resolve(pidFile),
		StateFile:   resolve(stateFile),
		LogFile:     resolve(logFile),
	}, nil
}

func buildWatcherConfigFromProject(cmd *cobra.Command, projectPath string) (watcher.Config, error) {
	return buildWatcherConfig(cmd, []string{projectPath})
}

func discoverIndexedProjects() ([]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	projects := map[string]struct{}{}
	skipRoots := map[string]struct{}{
		".git":         {},
		"node_modules": {},
		"vendor":       {},
		".cache":       {},
		"Library":      {},
		".Trash":       {},
	}

	err = filepath.WalkDir(home, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if _, skip := skipRoots[name]; skip && path != home {
				return filepath.SkipDir
			}
			if name == ".codelens" {
				dbPath := filepath.Join(path, "index.db")
				if _, err := os.Stat(dbPath); err == nil {
					if project := filepath.Dir(path); project != "" {
						projects[project] = struct{}{}
					}
				}
				return filepath.SkipDir
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	out := make([]string, 0, len(projects))
	for p := range projects {
		out = append(out, p)
	}
	sort.Strings(out)
	return out, nil
}

type Config struct {
	ProjectPath string
	DBPath      string
	OllamaURL   string
	OllamaModel string
}

func loadConfig() Config {
	projectPath := viper.GetString("project")
	dbPath := viper.GetString("db")

	// Si dbPath est relatif, le mettre dans le projet
	if !isAbsPath(dbPath) {
		dbPath = projectPath + "/" + dbPath
	}

	return Config{
		ProjectPath: projectPath,
		DBPath:      dbPath,
		OllamaURL:   viper.GetString("ollama-url"),
		OllamaModel: viper.GetString("ollama-model"),
	}
}

func isAbsPath(p string) bool { return len(p) > 0 && p[0] == '/' }

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
