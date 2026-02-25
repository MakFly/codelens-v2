package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
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

var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
	builtBy = "unknown"
)

var rootCmd = &cobra.Command{
	Use:   "codelens",
	Short: "Agentic memory & semantic search MCP server for Claude Code",
	Long: `CodeLens - Agentic Memory & Semantic Search

A tool that provides Claude Code with semantic search over your codebase
and persistent memory for team insights.

Quick Start:
  codelens update                     # Update to latest release
  codelens index .                    # Index current project
  codelens serve                      # Start MCP server (stdio)
  codelens stats                     # Show index statistics
  codelens watcher start .           # Start background watcher

Environment Variables:
  CODELENS_OLLAMA_URL       Ollama server URL (default: http://127.0.0.1:11434)
  CODELENS_OLLAMA_MODEL     Embedding model (default: nomic-embed-text)
  CODELENS_PROFILE          Runtime profile: auto|low|balanced|high (default: auto)
  CODELENS_MAX_CPU_THREADS  Max CPU threads per embedding request (0=use profile default)
  CODELENS_DB              SQLite database path (default: .codelens/index.db)
  CODELENS_PROJECT         Project root directory (default: .)

For more help, see: https://github.com/MakFly/codelens-v2`,
}

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the MCP server (stdio transport)",
	Long: `Start the CodeLens MCP server using stdio transport.

This command is typically used by AI clients (Claude Code, OpenCode, etc.)
via MCP configuration. It starts a long-running server that listens for
JSON-RPC requests on stdio.

The server exposes these MCP tools:
  - search_codebase   Semantic search over indexed code
  - read_file_smart   Read file sections with context
  - remember          Store insights in team memory
  - recall            Retrieve relevant memories
  - propose_memory    Create memory proposals for review
  - publish_memory    Publish approved memories
  - index_status      Get current index state

Example:
  codelens serve

For Claude Code integration, use the included installer or manually
configure ~/.claude/settings.json with the MCP server.`,
	RunE: runServe,
}

var indexCmd = &cobra.Command{
	Use:   "index [path]",
	Short: "Index a project directory for semantic search",
	Long: `Index a project directory to enable semantic search and code analysis.

This command scans all source files in the specified directory, splits them
into semantic chunks using tree-sitter, and generates embeddings using Ollama.

The indexer automatically skips:
  - Hidden directories (.git, .codelens, etc.)
  - Dependencies (node_modules, vendor, etc.)
  - Build artifacts (*.pyc, *.o, etc.)
  - Binary files and large files

Indexing Process:
  1. Walk directory tree (respects skip patterns)
  2. Detect language and parse with tree-sitter
  3. Split into semantic chunks (functions, classes, etc.)
  4. Generate embeddings via Ollama
  5. Store in SQLite + HNSW index

The --force flag re-indexes all files even if unchanged.
The --watch flag starts file watching after initial index.
Runtime profile is auto-tuned by default; override with --profile and --max-cpu-threads.

Examples:
  codelens index .                         # Index current directory
  codelens index /path/to/project          # Index specific path
  codelens index . --force                 # Force re-index all files
  codelens index . --watch                 # Index and watch for changes`,
	Args: cobra.MaximumNArgs(1),
	RunE: runIndex,
}

var searchCmd = &cobra.Command{
	Use:   "search [query]",
	Short: "Search the codebase using semantic similarity",
	Long: `Search the indexed codebase using natural language queries.

This command performs semantic search using vector similarity. It converts
your query into an embedding and finds the most relevant code chunks.

The search returns:
  - File path and line numbers
  - Similarity score (0-1, higher is better)
  - Code symbol info (function, class, etc.)
  - Content snippet

Examples:
  codelens search "authentication logic"
  codelens search "database connection setup"
  codelens search "user validation function"

Tips:
  - Use natural language, not just keywords
  - Be specific: "login handler" vs "auth"
  - Include context: "React useState hook"`,
	Args: cobra.ExactArgs(1),
	RunE: runSearch,
}

var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show index statistics and health",
	Long: `Display statistics about the current project index.

Shows:
  - Total indexed files
  - Total code chunks
  - Number of failed files
  - Active team memories
  - Last index timestamp
  - Database location

Use this to verify indexing completed successfully and to monitor
index health over time.`,
	RunE: runStats,
}

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update CodeLens to latest release",
	Long: `Update CodeLens by downloading and running the official install script.

By default, this command installs the latest GitHub release into ~/.local/bin.
You can pin a specific version with --version and choose a custom install path
with --install-dir.`,
	RunE: runUpdate,
}

var watcherCmd = &cobra.Command{
	Use:   "watcher",
	Short: "Manage background file watcher daemon",
	Long: `Manage a background daemon that watches for file changes and
automatically updates the index.

The watcher monitors your project directory in real-time using fsnotify
and triggers incremental re-indexing when files change. This ensures
your semantic search index stays up-to-date without manual intervention.

Commands:
  start     Start watcher daemon in background
  stop      Stop running watcher daemon
  status    Show watcher status (running/stopped)

The watcher runs as a background process and persists its PID to a file.
It includes:
  - Real-time file watching (fsnotify, no polling)
  - Concurrent cycle protection (mutex)
  - Stale state recovery after crashes
  - File lock detection (skip files being edited)
  - Debounced updates (500ms)

Examples:
  codelens watcher start .                # Start watcher for current project
  codelens watcher start /path/to/proj   # Start watcher for specific project
  codelens watcher stop                   # Stop all watchers
  codelens watcher status                 # Show all watcher statuses

Environment Variables:
  CODELENS_SKIP_LOCK_CHECK   Set to "1" to disable file lock detection`,
	RunE: runWatcherAuto,
}

var watcherStartCmd = &cobra.Command{
	Use:   "start [path]",
	Short: "Start watcher daemon in background",
	Long: `Start the background watcher daemon for a project.

The watcher will:
  - Run in background (detached process)
  - Monitor files using fsnotify (real-time)
  - Trigger index updates on file changes
  - Log to .codelens/watcher.log

Without arguments, starts watchers for all discovered indexed projects.

With a path argument, starts watcher for that specific project.

Examples:
  codelens watcher start .                     # Start for current dir
  codelens watcher start /path/to/project     # Start for specific path
  codelens watcher start . --interval 10s      # Custom interval`,
	Args: cobra.MaximumNArgs(1),
	RunE: runWatcherStart,
}

var watcherStopCmd = &cobra.Command{
	Use:   "stop [path]",
	Short: "Stop watcher daemon(s)",
	Long: `Stop the running watcher daemon.

Without arguments, stops all watchers found in the user's home directory.
With a path argument, stops the watcher for that specific project.

The command sends SIGTERM to the watcher process and cleans up PID files.

Examples:
  codelens watcher stop              # Stop all watchers
  codelens watcher stop .           # Stop watcher for current dir
  codelens watcher stop /path/proj  # Stop specific watcher`,
	Args: cobra.MaximumNArgs(1),
	RunE: runWatcherStop,
}

var watcherStatusCmd = &cobra.Command{
	Use:   "status [path]",
	Short: "Show watcher daemon status",
	Long: `Display the current status of watcher daemon(s).

Shows detailed state including:
  - Running status (true/false)
  - Process ID
  - Last cycle timestamp
  - Files/chunks indexed
  - Any errors

Without arguments, shows status for all discovered projects.
With a path, shows status for that specific project.

Examples:
  codelens watcher status              # Status of all watchers
  codelens watcher status .           # Status for current dir`,
	Args: cobra.MaximumNArgs(1),
	RunE: runWatcherStatus,
}

var watcherRunCmd = &cobra.Command{
	Use:    "run [path]",
	Short:  "Run watcher loop in foreground (internal)",
	Args:   cobra.MaximumNArgs(1),
	RunE:   runWatcherRun,
	Hidden: true,
}

func init() {
	rootCmd.PersistentFlags().String("project", ".", "Project root directory (auto-detected from cwd if flag/env not explicitly set)")
	rootCmd.PersistentFlags().String("db", ".codelens/index.db", "SQLite database path (relative to project)")
	rootCmd.PersistentFlags().String("ollama-url", "http://127.0.0.1:11434", "Ollama API server URL")
	rootCmd.PersistentFlags().String("ollama-model", "nomic-embed-text", "Ollama embedding model name")
	rootCmd.PersistentFlags().String("profile", "auto", "Runtime profile: auto|low|balanced|high")
	rootCmd.PersistentFlags().Int("max-cpu-threads", 0, "Max CPU threads per embedding request (0=use profile default)")

	viper.BindPFlags(rootCmd.PersistentFlags())
	viper.AutomaticEnv()
	viper.SetEnvPrefix("CODELENS")

	indexCmd.Flags().BoolP("watch", "w", false, "Watch for file changes after initial index (Ctrl+C to stop)")
	indexCmd.Flags().BoolP("force", "f", false, "Re-index all files even if hash unchanged")
	updateCmd.Flags().String("version", "latest", "Version to install: latest or semver (e.g. 0.2.3)")
	updateCmd.Flags().String("install-dir", "", "Install directory (default: ~/.local/bin)")

	watcherCmd.PersistentFlags().Duration("interval", 5*time.Second, "Watcher re-index interval (e.g., 5s, 1m)")
	watcherCmd.PersistentFlags().BoolP("force", "f", false, "Force full index at watcher startup")
	watcherCmd.PersistentFlags().String("pid-file", ".codelens/watcher.pid", "Watcher PID file path")
	watcherCmd.PersistentFlags().String("state-file", ".codelens/watcher.state.json", "Watcher state file path")
	watcherCmd.PersistentFlags().String("log-file", ".codelens/watcher.log", "Watcher log file path")

	watcherCmd.AddCommand(watcherStartCmd, watcherStopCmd, watcherStatusCmd, watcherRunCmd)
	rootCmd.AddCommand(serveCmd, indexCmd, searchCmd, statsCmd, updateCmd, watcherCmd)

	rootCmd.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Show CodeLens version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("CodeLens %s\n", version)
			if commit != "unknown" {
				fmt.Printf("Commit: %s\n", commit)
			}
			if date != "unknown" {
				fmt.Printf("Built: %s\n", date)
			}
			fmt.Println("Agentic memory & semantic search for Claude Code")
		},
	})
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runServe(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig(cmd)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Project resolution source: %s\n", cfg.ProjectSource)
	fmt.Fprintf(os.Stderr, "Resolved project: %s\n", cfg.ProjectPath)
	fmt.Fprintf(os.Stderr, "Resolved db: %s\n", cfg.DBPath)

	db, err := store.Open(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	embedder, _ := buildEmbedder(cfg)

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

	cfg, err := loadConfig(cmd)
	if err != nil {
		return err
	}
	watch, _ := cmd.Flags().GetBool("watch")
	force, _ := cmd.Flags().GetBool("force")

	db, err := store.Open(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	embedder, tuning := buildEmbedder(cfg)

	idx, err := indexer.New(path, db, embedder)
	if err != nil {
		return fmt.Errorf("create indexer: %w", err)
	}

	if tuning.InvalidProfile {
		fmt.Fprintf(os.Stderr, "Warning: invalid profile %q, using %q\n", tuning.RequestedProfile, tuning.Profile)
	}
	if tuning.ThreadsClamped {
		fmt.Fprintf(os.Stderr, "Warning: max-cpu-threads=%d clamped to %d (safety cap=%d)\n", tuning.RequestedThreads, tuning.NumThreads, tuning.HardCap)
	}
	fmt.Printf(
		"Runtime profile: %s | cpu=%d | mem=%dGiB | ollama_threads=%d | max_concurrent=%d\n",
		tuning.Profile, tuning.CPUs, tuning.MemoryGiB, tuning.NumThreads, tuning.MaxConcurrent,
	)
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
	cfg, err := loadConfig(cmd)
	if err != nil {
		return err
	}

	db, err := store.Open(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	embedder, _ := buildEmbedder(cfg)

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
	cfg, err := loadConfig(cmd)
	if err != nil {
		return err
	}

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

func runUpdate(cmd *cobra.Command, args []string) error {
	target, _ := cmd.Flags().GetString("version")
	installDir, _ := cmd.Flags().GetString("install-dir")

	if strings.TrimSpace(installDir) == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("resolve home dir: %w", err)
		}
		installDir = filepath.Join(home, ".local", "bin")
	}

	target = strings.TrimSpace(target)
	if target == "" {
		target = "latest"
	}

	release, err := fetchRelease("MakFly/codelens-v2", target)
	if err != nil {
		return fmt.Errorf("fetch release metadata: %w", err)
	}
	current := version
	if commit != "unknown" && current != "dev" {
		current = fmt.Sprintf("%s (%s)", current, commit)
	}

	tag := strings.TrimSpace(release.TagName)
	ver := strings.TrimPrefix(tag, "v")
	goos, goarch := detectReleasePlatform()
	assetName := fmt.Sprintf("codelens_%s_%s_%s.tar.gz", ver, goos, goarch)
	assetURL := ""
	for _, a := range release.Assets {
		if a.Name == assetName {
			assetURL = a.BrowserDownloadURL
			break
		}
	}
	if assetURL == "" {
		return fmt.Errorf("release %s does not contain asset %q", tag, assetName)
	}

	fmt.Printf("Current version: %s\n", current)
	fmt.Printf("Latest release:  %s\n", tag)
	fmt.Printf("Installing:      %s\n", assetName)
	fmt.Printf("Install dir:     %s\n\n", installDir)

	if err := os.MkdirAll(installDir, 0o755); err != nil {
		return fmt.Errorf("prepare install dir: %w", err)
	}
	if err := downloadAndExtractTarGz(assetURL, installDir); err != nil {
		return fmt.Errorf("install release asset: %w", err)
	}

	fmt.Printf("\n✓ Update completed. Verify with: %s/codelens version\n", installDir)
	return nil
}

type githubRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

func fetchRelease(repo, target string) (*githubRelease, error) {
	url := ""
	if target == "latest" {
		url = fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)
	} else {
		tag := target
		if !strings.HasPrefix(tag, "v") {
			tag = "v" + tag
		}
		url = fmt.Sprintf("https://api.github.com/repos/%s/releases/tags/%s", repo, tag)
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "codelens-cli")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("github api status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	var payload githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	if strings.TrimSpace(payload.TagName) == "" {
		return nil, fmt.Errorf("empty tag_name in release payload")
	}
	return &payload, nil
}

func detectReleasePlatform() (string, string) {
	goos := runtime.GOOS
	switch goos {
	case "linux", "darwin":
	default:
		goos = "linux"
	}

	goarch := runtime.GOARCH
	switch goarch {
	case "amd64", "arm64":
	default:
		goarch = "amd64"
	}
	return goos, goarch
}

func downloadAndExtractTarGz(url, installDir string) error {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "codelens-cli")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("download status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	gzr, err := gzip.NewReader(resp.Body)
	if err != nil {
		return err
	}
	defer gzr.Close()
	tr := tar.NewReader(gzr)

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}

		base := filepath.Base(hdr.Name)
		if base != "codelens" && base != "codelens-hook" {
			continue
		}
		dst := filepath.Join(installDir, base)
		out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, tr); err != nil {
			_ = out.Close()
			return err
		}
		if err := out.Close(); err != nil {
			return err
		}
		if err := os.Chmod(dst, 0o755); err != nil {
			return err
		}
	}
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

	embedder, _ := buildEmbedder(Config{
		OllamaURL:     cfg.OllamaURL,
		OllamaModel:   cfg.OllamaModel,
		Profile:       cfg.Profile,
		MaxCPUThreads: cfg.MaxCPUThreads,
	})
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
	cfg, err := loadConfig(cmd)
	if err != nil {
		return watcher.Config{}, err
	}
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
		ProjectPath:   absProject,
		DBPath:        dbPath,
		OllamaURL:     cfg.OllamaURL,
		OllamaModel:   cfg.OllamaModel,
		Profile:       cfg.Profile,
		MaxCPUThreads: cfg.MaxCPUThreads,
		Interval:      interval,
		Force:         force,
		PIDFile:       resolve(pidFile),
		StateFile:     resolve(stateFile),
		LogFile:       resolve(logFile),
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
	ProjectPath   string
	ProjectSource string
	DBPath        string
	OllamaURL     string
	OllamaModel   string
	Profile       string
	MaxCPUThreads int
}

type ProjectResolution struct {
	Path   string
	Source string
}

func loadConfig(cmd *cobra.Command) (Config, error) {
	projectFlagValue := viper.GetString("project")
	projectFlagChanged := false
	if cmd != nil {
		projectFlagChanged = cmd.Flags().Changed("project")
	}

	envProjectValue, envProjectSet := os.LookupEnv("CODELENS_PROJECT")
	cwd, err := os.Getwd()
	if err != nil {
		return Config{}, fmt.Errorf("resolve cwd: %w", err)
	}

	project, err := resolveProjectPath(projectFlagValue, projectFlagChanged, envProjectValue, envProjectSet, cwd)
	if err != nil {
		return Config{}, err
	}

	dbPath, err := resolveDBPath(project.Path, viper.GetString("db"))
	if err != nil {
		return Config{}, err
	}

	return Config{
		ProjectPath:   project.Path,
		ProjectSource: project.Source,
		DBPath:        dbPath,
		OllamaURL:     viper.GetString("ollama-url"),
		OllamaModel:   viper.GetString("ollama-model"),
		Profile:       viper.GetString("profile"),
		MaxCPUThreads: viper.GetInt("max-cpu-threads"),
	}, nil
}

func buildEmbedder(cfg Config) (*embeddings.OllamaClient, RuntimeTuning) {
	e := embeddings.NewOllama(cfg.OllamaURL, cfg.OllamaModel)
	t := ResolveRuntimeTuning(cfg.Profile, cfg.MaxCPUThreads)
	e.SetNumThreads(t.NumThreads)
	e.SetMaxConcurrent(t.MaxConcurrent)
	return e, t
}

func resolveProjectPath(projectFlagValue string, projectFlagChanged bool, envProjectValue string, envProjectSet bool, cwd string) (ProjectResolution, error) {
	cleanCWD := strings.TrimSpace(cwd)
	if cleanCWD == "" {
		return ProjectResolution{}, fmt.Errorf("empty cwd")
	}

	if projectFlagChanged {
		flagValue := strings.TrimSpace(projectFlagValue)
		if flagValue == "" {
			return ProjectResolution{}, fmt.Errorf("project flag is empty")
		}
		abs, err := filepath.Abs(flagValue)
		if err != nil {
			return ProjectResolution{}, fmt.Errorf("resolve project flag path: %w", err)
		}
		return ProjectResolution{Path: filepath.Clean(abs), Source: "flag"}, nil
	}

	if envProjectSet {
		envValue := strings.TrimSpace(envProjectValue)
		if envValue != "" {
			abs, err := filepath.Abs(envValue)
			if err != nil {
				return ProjectResolution{}, fmt.Errorf("resolve CODELENS_PROJECT path: %w", err)
			}
			return ProjectResolution{Path: filepath.Clean(abs), Source: "env"}, nil
		}
	}

	auto, found, err := findNearestIndexedProject(cleanCWD)
	if err != nil {
		return ProjectResolution{}, err
	}
	if found {
		return ProjectResolution{Path: auto, Source: "auto"}, nil
	}

	absCWD, err := filepath.Abs(cleanCWD)
	if err != nil {
		return ProjectResolution{}, fmt.Errorf("resolve fallback cwd path: %w", err)
	}
	return ProjectResolution{Path: filepath.Clean(absCWD), Source: "fallback"}, nil
}

func findNearestIndexedProject(start string) (string, bool, error) {
	absStart, err := filepath.Abs(start)
	if err != nil {
		return "", false, fmt.Errorf("resolve start path: %w", err)
	}
	current := filepath.Clean(absStart)

	for {
		dbPath := filepath.Join(current, ".codelens", "index.db")
		if fi, statErr := os.Stat(dbPath); statErr == nil && !fi.IsDir() {
			return current, true, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", false, nil
		}
		current = parent
	}
}

func resolveDBPath(projectPath, dbValue string) (string, error) {
	project := strings.TrimSpace(projectPath)
	if project == "" {
		return "", fmt.Errorf("project path is empty")
	}

	db := strings.TrimSpace(dbValue)
	if db == "" {
		db = ".codelens/index.db"
	}
	if filepath.IsAbs(db) {
		return filepath.Clean(db), nil
	}
	return filepath.Join(project, db), nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
