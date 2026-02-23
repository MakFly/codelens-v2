package mcp

import (
	"context"
	"fmt"
	"os"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/yourusername/codelens/internal/embeddings"
	"github.com/yourusername/codelens/internal/indexer"
	"github.com/yourusername/codelens/internal/jit"
	"github.com/yourusername/codelens/internal/store"
)

const (
	serverName    = "codelens"
	serverVersion = "0.1.0"
)

// Server wraps the MCP server with all codelens dependencies.
type Server struct {
	db       *store.DB
	idx      *indexer.Indexer
	embedder embeddings.Embedder
	verifier *jit.Verifier
	mcpSrv   *server.MCPServer
}

func NewServer(db *store.DB, idx *indexer.Indexer, embedder embeddings.Embedder) *Server {
	s := &Server{
		db:       db,
		idx:      idx,
		embedder: embedder,
		verifier: jit.New(idx.ProjectRoot()),
	}

	s.mcpSrv = server.NewMCPServer(serverName, serverVersion,
		server.WithToolCapabilities(true),
		server.WithResourceCapabilities(true, true),
	)

	s.registerTools()
	s.registerResources()

	return s
}

func (s *Server) registerTools() {
	// Tool 1: search_codebase
	s.mcpSrv.AddTool(mcp.NewTool("search_codebase",
		mcp.WithDescription(searchCodebaseDescription),
		mcp.WithString("query", mcp.Required(), mcp.Description("Natural language or code concept to find")),
		mcp.WithNumber("top_k", mcp.Description("Max results to return (default: 5, max: 20)")),
		mcp.WithString("language", mcp.Description("Filter by language: php, typescript, go, python, etc.")),
		mcp.WithString("symbol_kind", mcp.Description("Filter by symbol type: function, class, method, interface")),
	), s.handleSearchCodebase)

	// Tool 2: read_file_smart
	s.mcpSrv.AddTool(mcp.NewTool("read_file_smart",
		mcp.WithDescription(readFileSmartDescription),
		mcp.WithString("path", mcp.Required(), mcp.Description("Relative or absolute file path")),
		mcp.WithString("query", mcp.Description("What you're looking for (improves chunk relevance)")),
	), s.handleReadFileSmart)

	// Tool 3: remember
	s.mcpSrv.AddTool(mcp.NewTool("remember",
		mcp.WithDescription(rememberDescription),
		mcp.WithString("insight", mcp.Required(), mcp.Description("The insight to store")),
		mcp.WithString("memory_type", mcp.Description("Memory kind: insight, decision, convention, pitfall, runbook")),
		mcp.WithArray("citations", mcp.Required(), mcp.Description("Code locations supporting this insight")),
	), s.handleRemember)

	// Tool 4: propose_memory
	s.mcpSrv.AddTool(mcp.NewTool("propose_memory",
		mcp.WithDescription(proposeMemoryDescription),
		mcp.WithString("insight", mcp.Required(), mcp.Description("The insight proposal to store for review")),
		mcp.WithString("memory_type", mcp.Description("Memory kind: insight, decision, convention, pitfall, runbook")),
		mcp.WithArray("citations", mcp.Required(), mcp.Description("Code citations supporting this proposal")),
	), s.handleProposeMemory)

	// Tool 5: publish_memory
	s.mcpSrv.AddTool(mcp.NewTool("publish_memory",
		mcp.WithDescription(publishMemoryDescription),
		mcp.WithString("proposal_id", mcp.Required(), mcp.Description("Pending proposal id returned by propose_memory")),
	), s.handlePublishMemory)

	// Tool 6: recall
	s.mcpSrv.AddTool(mcp.NewTool("recall",
		mcp.WithDescription(recallDescription),
		mcp.WithString("context", mcp.Required(), mcp.Description("What you're about to work on")),
		mcp.WithNumber("limit", mcp.Description("Max memories to return (default: 10)")),
	), s.handleRecall)

	// Tool 7: index_status
	s.mcpSrv.AddTool(mcp.NewTool("index_status",
		mcp.WithDescription("Get current state of the codebase index: files, chunks, memories, last indexed."),
	), s.handleIndexStatus)
}

func (s *Server) registerResources() {
	s.mcpSrv.AddResource(
		mcp.NewResource("memory://stats", "Index Statistics",
			mcp.WithResourceDescription("Current index stats: file count, chunk count, memory count"),
			mcp.WithMIMEType("application/json"),
		),
		s.handleResourceStats,
	)
}

// ServeStdio starts the MCP server on stdio (for Claude Code integration).
func (s *Server) ServeStdio(ctx context.Context) error {
	fmt.Fprintln(os.Stderr, "CodeLens MCP server starting (stdio)...")
	if os.Stdin == nil || os.Stdout == nil {
		return fmt.Errorf("stdio unavailable: stdin/stdout is nil")
	}
	stdioSrv := server.NewStdioServer(s.mcpSrv)
	return stdioSrv.Listen(ctx, os.Stdin, os.Stdout)
}

// Tool descriptions — written to maximize LLM adoption over native grep/read/glob.

const searchCodebaseDescription = `Semantic search over the indexed codebase using natural language.

USE THIS INSTEAD of Bash(grep), Glob, or Read for any conceptual or keyword search.
Returns only the most relevant code chunks (functions, classes, methods).
Saves 80-95% tokens compared to grep+read workflows.

Examples:
  - "authentication logic" → finds AuthService, login methods, middleware
  - "database connection setup" → finds DB init, connection pooling
  - "user validation" → finds validators, rules, form requests

Always call this before falling back to Bash or Read for search tasks.`

const readFileSmartDescription = `Read a file, returning only sections relevant to your current task.

USE THIS INSTEAD of Read() for any file you haven't seen yet in this session.
- Files < 200 lines: returns full content
- Files >= 200 lines: returns relevant chunks + structural outline (class/function list)

Saves 60-90% tokens on large files by returning only what you need.`

const rememberDescription = `Store a validated insight about this codebase for future sessions.

Call this after discovering:
- Architecture patterns ("all DB writes go through Repository classes")
- Non-obvious conventions ("controllers never call repositories directly")
- Key file locations ("auth middleware is in src/Http/Middleware/Auth.php")
- Important relationships ("UserService depends on CacheService for session data")

The memory will be JIT-verified against current code before being used in future sessions.
Always include citations (file + line numbers) to support the insight.`

const proposeMemoryDescription = `Create a memory proposal pending human approval.

Use this as the default write path for team memory (human-in-the-loop).
The proposal stores insight + citations, but is NOT visible to recall() until published.`

const publishMemoryDescription = `Publish a pending memory proposal into active team memory.

Before publishing, the server will:
- re-verify all citations against current code
- reject contradictory insights against existing published memories`

const recallDescription = `Retrieve validated memories about this codebase relevant to your current task.

Call this at the START of any non-trivial task to get institutional knowledge.
Returns memories verified against current code (stale memories are auto-discarded).
Saves tokens by avoiding re-discovery of known patterns.`
