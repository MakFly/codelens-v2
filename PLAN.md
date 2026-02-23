# CodeLens — MCP Agentic Memory & Semantic Search
## Plan Complet de Conception v1.0

> **Objectif** : Réduire de 70-90% les tokens consommés par Claude Code (et tout agent CLI MCP-compatible) lors de l'exploration de codebase, via un système à 3 couches : shadow tools MCP, interception de hooks, et mémoire agentique persistante avec vérification JIT.

---

## 1. Vue d'ensemble du système

```
┌──────────────────────────────────────────────────────────────┐
│                    Claude Code / Agent CLI                   │
│                   (session, task, context)                   │
└───────────────────────┬──────────────────────────────────────┘
                        │ MCP Protocol (stdio/SSE)
          ┌─────────────▼──────────────┐
          │      Layer 3 : MCP Server   │  ← Shadow tools
          │  search_codebase()          │    (remplace grep/read/glob)
          │  read_file_smart()          │
          │  remember() / recall()      │
          │  index_status()             │
          └─────────────┬──────────────┘
                        │
          ┌─────────────▼──────────────┐
          │    Query Router + Ranker   │
          └──────┬──────────┬──────────┘
                 │          │
    ┌────────────▼──┐  ┌────▼────────────────┐
    │ Vector Index  │  │   Memory Store      │
    │ HNSW + SQLite │  │   SQLite + JIT      │
    │ (embeddings)  │  │   (insights+cites)  │
    └────────────┬──┘  └────────────────────┘
                 │
    ┌────────────▼──────────────┐
    │   Indexer (background)    │
    │   fsnotify watcher        │
    │   go-tree-sitter chunker  │
    │   Ollama embeddings client│
    └───────────────────────────┘

          ┌─────────────────────────────┐
          │   Layer 2 : Hook Binary     │  ← Intercepte grep/read/glob
          │   codelens-hook             │    qd l'agent fallback
          │   PreToolUse handler        │
          └─────────────────────────────┘

          ┌─────────────────────────────┐
          │   Layer 1 : CLAUDE.md       │  ← Instructions prompt-level
          └─────────────────────────────┘
```

---

## 2. Stack technique

| Composant           | Choix                          | Justification                                          |
|---------------------|--------------------------------|--------------------------------------------------------|
| Langage             | **Go 1.22+**                   | Binaire unique, iteration rapide, excellent stdlib     |
| MCP SDK             | `mark3labs/mcp-go`             | SDK officiel Go, stdio + SSE, bien maintenu            |
| File watching       | `fsnotify/fsnotify`            | Cross-platform, stable                                 |
| AST Chunking        | `smacker/go-tree-sitter`       | Bindings Go pour tree-sitter (PHP, TS, JS, Go, Python) |
| Language detection  | `go-enry/go-enry`              | Port Go de linguist GitHub                             |
| Embeddings          | **Ollama HTTP API**            | Local, modèle `nomic-embed-text` (768 dims)            |
| Vector search       | `coder/hnsw`                   | HNSW pure Go, pas de C, performant jusqu'à ~100k chunks|
| Storage             | `mattn/go-sqlite3`             | Memory store + metadata + hash cache                   |
| Hashing             | `cespare/xxhash`               | Ultra rapide pour JIT verification                     |
| Config              | `spf13/viper`                  | YAML config, env vars                                  |
| CLI                 | `spf13/cobra`                  | Commands: serve, index, search, stats                  |
| Tests               | `testify/testify`              | Assertions + mocks                                     |

---

## 3. Structure du projet

```
codelens-v2/
├── cmd/
│   ├── codelens/
│   │   └── main.go               # Entrypoint MCP server + CLI
│   └── hook/
│       └── main.go               # Binary hook interceptor
│
├── internal/
│   ├── indexer/
│   │   ├── indexer.go            # Orchestrateur d'indexation
│   │   ├── watcher.go            # fsnotify file watcher
│   │   ├── chunker.go            # Router par langage
│   │   ├── chunker_treesitter.go # AST-aware chunking (PHP/TS/JS/Go/Py)
│   │   ├── chunker_generic.go    # Fallback chunking par blocs
│   │   └── languages.go          # Détection + config par langage
│   │
│   ├── embeddings/
│   │   ├── client.go             # Interface Embedder
│   │   ├── ollama.go             # Implémentation Ollama
│   │   └── mock.go               # Mock pour tests (sans Ollama)
│   │
│   ├── store/
│   │   ├── schema.go             # DDL SQLite + migrations
│   │   ├── chunks.go             # CRUD chunks + embeddings
│   │   ├── memory.go             # CRUD memories + citations
│   │   └── stats.go              # Stats index
│   │
│   ├── vector/
│   │   ├── index.go              # HNSW wrapper + persist/load
│   │   └── similarity.go         # Cosine similarity utils
│   │
│   ├── jit/
│   │   └── verifier.go           # JIT citation verifier
│   │
│   ├── mcp/
│   │   ├── server.go             # MCP server setup + lifecycle
│   │   ├── tools.go              # Définition des 5 tools
│   │   ├── resources.go          # Resources MCP (stats, memories)
│   │   └── handler_*.go          # Handler par tool
│   │
│   └── hook/
│       ├── interceptor.go        # Logique d'interception
│       ├── bash_handler.go       # Analyse + reroute Bash(grep/find)
│       └── read_handler.go       # Chunking smart pour Read large
│
├── config/
│   └── default.yaml              # Config par défaut
│
├── .claude/
│   └── settings.json             # Claude Code hooks configuration
│
├── CLAUDE.md                     # Instructions injectées dans chaque session
├── go.mod
├── go.sum
├── Makefile
└── README.md
```

---

## 4. Modèles de données

### 4.1 Chunk (unité d'indexation)

```go
// internal/indexer/chunker.go
type Chunk struct {
    ID        string    // SHA256(filepath + start_line)
    FilePath  string    // Chemin relatif depuis la racine du projet
    StartLine int
    EndLine   int
    Content   string
    Language  string    // "php", "typescript", "go", etc.
    Symbol    string    // Nom de la fonction/classe si détecté (AST)
    SymbolKind string   // "function", "class", "method", "interface"
    Hash      string    // xxhash du Content (pour invalidation)
    IndexedAt time.Time
}
```

### 4.2 VectorRecord (stockage)

```go
// internal/store/chunks.go
type VectorRecord struct {
    ChunkID   string
    Embedding []float32  // 768 dims pour nomic-embed-text
    FilePath  string
    StartLine int
    EndLine   int
    Content   string
    Hash      string
}
```

### 4.3 Memory (mémoire agentique)

```go
// internal/store/memory.go
type Memory struct {
    ID        string
    Insight   string     // Le contenu de la mémoire (texte libre)
    Citations []Citation // Pointeurs vers le code source
    CreatedAt time.Time
    ExpiresAt time.Time  // Par défaut: CreatedAt + 28 jours
    HitCount  int        // Nb de fois utilisé
    LastUsed  time.Time
}

type Citation struct {
    FilePath  string
    LineStart int
    LineEnd   int
    Hash      string     // xxhash des lignes citées au moment de création
                         // Utilisé par JIT pour détecter les changements
}
```

### 4.4 Schema SQLite

```sql
-- Chunks indexés
CREATE TABLE chunks (
    id         TEXT PRIMARY KEY,
    file_path  TEXT NOT NULL,
    start_line INTEGER NOT NULL,
    end_line   INTEGER NOT NULL,
    content    TEXT NOT NULL,
    language   TEXT,
    symbol     TEXT,
    symbol_kind TEXT,
    hash       TEXT NOT NULL,
    indexed_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_chunks_file ON chunks(file_path);
CREATE INDEX idx_chunks_language ON chunks(language);

-- Embeddings (HNSW est en mémoire, SQLite sert de persistence)
CREATE TABLE embeddings (
    chunk_id  TEXT PRIMARY KEY REFERENCES chunks(id) ON DELETE CASCADE,
    vector    BLOB NOT NULL  -- []float32 sérialisé en little-endian
);

-- Mémoires agentiques
CREATE TABLE memories (
    id         TEXT PRIMARY KEY,
    insight    TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    expires_at DATETIME NOT NULL,
    hit_count  INTEGER DEFAULT 0,
    last_used  DATETIME
);

-- Citations (relation mémoire → code)
CREATE TABLE citations (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    memory_id  TEXT NOT NULL REFERENCES memories(id) ON DELETE CASCADE,
    file_path  TEXT NOT NULL,
    line_start INTEGER NOT NULL,
    line_end   INTEGER NOT NULL,
    hash       TEXT NOT NULL    -- hash des lignes au moment de la création
);
CREATE INDEX idx_citations_memory ON citations(memory_id);

-- Hash cache pour invalidation rapide
CREATE TABLE file_hashes (
    file_path   TEXT PRIMARY KEY,
    hash        TEXT NOT NULL,
    updated_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

---

## 5. MCP Tools — Spécification complète

### Tool 1 : `search_codebase`

**Remplace** : `Bash(grep -r ...)`, `Glob(...)` + `Read(...)` chainés

```json
{
  "name": "search_codebase",
  "description": "Semantic search over the indexed codebase. USE THIS INSTEAD of grep, Glob, or Read for any conceptual search. Returns only the most relevant code chunks (functions, classes, methods), saving 80-95% tokens compared to file-based searches. Always try this before falling back to Bash or Read.",
  "inputSchema": {
    "type": "object",
    "properties": {
      "query":    { "type": "string",  "description": "Natural language or code concept to find" },
      "top_k":   { "type": "integer", "description": "Max results (default: 5, max: 20)" },
      "language":{ "type": "string",  "description": "Filter by language: php, typescript, go, python, etc." },
      "symbol_kind": { "type": "string", "description": "Filter by symbol: function, class, method, interface" }
    },
    "required": ["query"]
  }
}
```

**Output** :
```json
{
  "results": [
    {
      "file": "src/Auth/AuthService.php",
      "lines": "45-89",
      "symbol": "AuthService::login",
      "language": "php",
      "score": 0.94,
      "content": "public function login(string $email, string $password): Token\n{..."
    }
  ],
  "token_estimate": 420,
  "indexed_files": 1247,
  "query_time_ms": 38
}
```

---

### Tool 2 : `read_file_smart`

**Remplace** : `Read(file_path)` pour les fichiers > 200 lignes

```json
{
  "name": "read_file_smart",
  "description": "Read a file returning only the sections relevant to your current task. USE THIS INSTEAD of Read() for large files. For files under 200 lines, returns the full content. For larger files, returns relevant chunks + a structural outline.",
  "inputSchema": {
    "type": "object",
    "properties": {
      "path":  { "type": "string", "description": "Relative file path" },
      "query": { "type": "string", "description": "What you're looking for in this file (improves chunk selection)" }
    },
    "required": ["path"]
  }
}
```

---

### Tool 3 : `remember`

**Usage** : L'agent appelle ça après avoir découvert un pattern/convention

```json
{
  "name": "remember",
  "description": "Store a validated insight about this codebase for future sessions. Call this after discovering patterns, conventions, architecture decisions, or non-obvious code behavior. The memory will be auto-verified against the code before being used in future sessions.",
  "inputSchema": {
    "type": "object",
    "properties": {
      "insight":   { "type": "string", "description": "The insight to store (e.g., 'Authentication always goes through AuthService::login, never directly to UserRepository')" },
      "citations": {
        "type": "array",
        "description": "Code locations that support this insight",
        "items": {
          "type": "object",
          "properties": {
            "file":       { "type": "string"  },
            "line_start": { "type": "integer" },
            "line_end":   { "type": "integer" }
          },
          "required": ["file", "line_start", "line_end"]
        }
      }
    },
    "required": ["insight", "citations"]
  }
}
```

---

### Tool 4 : `recall`

**Usage** : Appelé en début de session ou avant de commencer une tâche

```json
{
  "name": "recall",
  "description": "Retrieve validated memories relevant to your current task. Call this at the start of any non-trivial task to get institutional knowledge about this codebase. Memories are JIT-verified against current code — stale memories are automatically discarded.",
  "inputSchema": {
    "type": "object",
    "properties": {
      "context": { "type": "string", "description": "What you're about to work on" },
      "limit":   { "type": "integer", "description": "Max memories to return (default: 10)" }
    },
    "required": ["context"]
  }
}
```

**Output** :
```json
{
  "memories": [
    {
      "id": "mem_a3f2",
      "insight": "Database transactions always use the TransactionManager service, never PDO directly",
      "citations": [
        { "file": "src/DB/TransactionManager.php", "lines": "12-45", "valid": true }
      ],
      "created_at": "2025-01-15",
      "hit_count": 7
    }
  ],
  "expired_count": 2,
  "total_memories": 14
}
```

---

### Tool 5 : `index_status`

```json
{
  "name": "index_status",
  "description": "Get the current state of the codebase index: indexed files, chunk count, memory count, last indexed timestamp.",
  "inputSchema": { "type": "object", "properties": {} }
}
```

---

## 6. Layer 2 — Hook Interceptor

### Configuration Claude Code

```json
// .claude/settings.json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [{
          "type": "command",
          "command": "codelens-hook bash"
        }]
      },
      {
        "matcher": "Read",
        "hooks": [{
          "type": "command",
          "command": "codelens-hook read"
        }]
      },
      {
        "matcher": "Glob",
        "hooks": [{
          "type": "command",
          "command": "codelens-hook glob"
        }]
      }
    ]
  }
}
```

### Protocole Hook (stdin/stdout)

Le binary `codelens-hook` reçoit sur **stdin** :
```json
{
  "tool_name": "Bash",
  "tool_input": { "command": "grep -r 'UserService' ./src --include='*.php'" },
  "session_id": "abc123"
}
```

Il retourne sur **stdout** (si reroute) ou **exit 0** (si laisse passer) :
```json
{
  "action": "block",
  "reason": "Intercepted grep — using semantic search instead",
  "result": "... chunks sémantiques ..."
}
```

### Logique d'interception Bash

```
Bash command reçu
       │
       ├── Contient grep/rg/find/fd sur le projet ?
       │   ├── OUI → extraire la query → semantic_search() → retourner chunks (exit 2 = block)
       │   └── NON → laisser passer (exit 0)
       │
       └── Contient cat/less/head sur un fichier ?
           ├── Fichier > 300 lignes → read_file_smart() → retourner chunks
           └── Sinon → laisser passer
```

### Logique d'interception Read

```
Read command reçu (file_path)
       │
       ├── Fichier > 500 lignes ? → read_file_smart(path) → exit 2 (block)
       ├── Fichier 200-500 lignes ? → read full + ajouter outline en header
       └── Fichier < 200 lignes → laisser passer (exit 0)
```

---

## 7. Layer 1 — CLAUDE.md

```markdown
# CodeLens — Instructions Agent

## Tool Priority (MANDATORY)

1. **search_codebase** : Use for ANY conceptual search before grep/Glob/Read
2. **read_file_smart** : Use instead of Read() for files you haven't opened yet
3. **recall** : Call at the START of any non-trivial task
4. **remember** : Call after discovering any non-obvious pattern or convention

## When to use each tool

| Situation | Tool | Never use |
|-----------|------|-----------|
| Finding where auth is handled | search_codebase("auth logic") | grep, Glob |
| Starting to work on payments | recall("payments feature") | nothing — just do it |
| Found a key architectural pattern | remember(insight, citations) | — |
| Reading a controller | read_file_smart(path, query) | Read() |

## Token budget awareness
- search_codebase returns ~400-800 tokens vs 15,000+ for grep+read
- recall returns ~500 tokens of institutional knowledge vs re-discovering it
- Never read entire files when you can search for the relevant section
```

---

## 8. Stratégie de chunking par langage

### PHP (tree-sitter `tree-sitter-php`)

Symboles extraits : `class_declaration`, `method_declaration`, `function_definition`, `interface_declaration`, `trait_declaration`

```
UserController.php
├── class UserController [lines 1-200]
│   ├── __construct() [lines 10-25]
│   ├── index() [lines 27-45]
│   ├── store() [lines 47-89]
│   └── update() [lines 91-130]
└── Helper functions en bas [lines 202-230]
```

### TypeScript/JavaScript (tree-sitter `tree-sitter-typescript`)

Symboles : `function_declaration`, `class_declaration`, `method_definition`, `arrow_function` (si assigné à const), `export_statement`

### Go (tree-sitter `tree-sitter-go`)

Symboles : `function_declaration`, `method_declaration`, `type_spec`, `interface_type`

### Fallback générique

Pour les langages sans grammaire tree-sitter configurée : découpe par blocs de 50 lignes avec overlap de 5 lignes.

### Taille des chunks

- **Minimum** : 10 lignes (évite les chunks trop petits inutiles)
- **Maximum** : 150 lignes (au-delà, on redécoupe)
- **Overlap** : 3 lignes entre chunks consécutifs du même fichier

---

## 9. Pipeline d'indexation

```
fsnotify event (file created/modified/deleted)
        │
        ▼
Language detection (go-enry)
        │
        ├── Supported language → tree-sitter chunker
        └── Unsupported → generic chunker
        │
        ▼
Pour chaque chunk :
  1. Calculer xxhash du contenu
  2. Comparer avec hash précédent en DB → skip si identique
  3. Appeler Ollama /api/embeddings (nomic-embed-text)
  4. Stocker chunk + embedding en SQLite
  5. Mettre à jour HNSW index en mémoire
        │
        ▼
Sauvegarder HNSW index sur disque (périodique, toutes les 5 min)
```

### Indexation initiale

```bash
codelens index ./               # Index complet depuis la racine
codelens index ./src            # Index partiel
codelens index --watch ./       # Index + watch mode (background)
```

### Performance attendue

| Codebase          | Fichiers | Chunks | Temps indexation | RAM (HNSW) |
|-------------------|----------|--------|------------------|------------|
| Petit projet      | 200      | ~2000  | ~30s             | ~50MB      |
| Projet moyen      | 1000     | ~10000 | ~3min            | ~250MB     |
| Gros monorepo     | 5000     | ~50000 | ~15min           | ~1.2GB     |

---

## 10. JIT Verifier — Détail d'implémentation

```go
// internal/jit/verifier.go

func (v *Verifier) VerifyMemory(mem Memory) (valid bool, corrected *Memory) {
    for _, cite := range mem.Citations {
        // 1. Lire les lignes citées actuellement
        currentLines, err := readLines(cite.FilePath, cite.LineStart, cite.LineEnd)
        if err != nil {
            // Fichier supprimé → mémoire invalide
            return false, nil
        }

        // 2. Calculer le hash actuel
        currentHash := xxhash.Sum64String(strings.Join(currentLines, "\n"))

        // 3. Comparer avec le hash au moment de création
        if strconv.FormatUint(currentHash, 16) != cite.Hash {
            // Code a changé → mémoire potentiellement stale
            // Option A : invalider complètement
            // Option B : demander à l'agent de corriger (via "corrected" memory)
            return false, nil
        }
    }
    return true, nil
}
```

---

## 11. Plan de tests

### 11.1 Tests unitaires

```
internal/indexer/chunker_test.go
  - TestChunkPHP_ClassWithMethods          // Parse une classe PHP avec 5 méthodes
  - TestChunkPHP_Interface                 // Interface sans implémentation
  - TestChunkTypeScript_ArrowFunction      // const foo = () => {}
  - TestChunkTypeScript_Class              // class UserService { ... }
  - TestChunkGeneric_LargeFile             // Fichier 500 lignes → blocs de 50
  - TestChunkGeneric_SmallFile             // Fichier 30 lignes → 1 chunk

internal/jit/verifier_test.go
  - TestVerify_ValidCitation               // Hash match → valid
  - TestVerify_ModifiedCode                // Hash mismatch → invalid
  - TestVerify_DeletedFile                 // Fichier absent → invalid
  - TestVerify_PartialInvalid              // 1 citation invalide sur 3 → invalid

internal/vector/index_test.go
  - TestHNSW_AddAndSearch                  // Insert 100 vecteurs + query
  - TestHNSW_PersistAndReload              // Sauvegarder + recharger depuis SQLite
  - TestCosineSimilarity                   // Calculs de précision

internal/hook/interceptor_test.go
  - TestIntercept_GrepCommand              // grep -r → doit intercepter
  - TestIntercept_RipgrepCommand           // rg → doit intercepter
  - TestIntercept_RegularBashCommand       // ls → doit laisser passer
  - TestIntercept_LargeFileRead            // Read 600 lignes → intercepter
  - TestIntercept_SmallFileRead            // Read 50 lignes → laisser passer
```

### 11.2 Tests d'intégration

```
test/integration/

search_integration_test.go
  - TestSearchReturnsRelevantChunks        // Ollama requis (tag: integration)
  - TestSearchFilterByLanguage
  - TestSearchFilterBySymbolKind

memory_integration_test.go
  - TestRememberAndRecall
  - TestRecallWithJITVerification
  - TestExpiredMemoriesNotReturned

indexer_integration_test.go
  - TestIndexAndSearch_RealProject         // Pointe sur codelens-v2 lui-même
  - TestIncrementalIndex_FileModified
  - TestIncrementalIndex_FileDeleted
```

### 11.3 Benchmarks

```
test/benchmark/

token_savings_test.go
  Scénario : 5 queries réelles sur un projet PHP/TS

  BenchmarkTokens_GrepApproach:
    query: "trouver la logique d'authentification"
    méthode: grep -r "auth" + read top 10 fichiers
    mesure: token count du résultat

  BenchmarkTokens_SemanticApproach:
    query: "trouver la logique d'authentification"
    méthode: search_codebase("authentication logic")
    mesure: token count du résultat

  Objectif: ratio >= 5x moins de tokens

latency_test.go
  BenchmarkSearchLatency_100Chunks
  BenchmarkSearchLatency_10000Chunks
  BenchmarkEmbeddingLatency_Ollama     // Mesure le RTT Ollama
  BenchmarkIndexing_FilesPerSecond
```

### 11.4 Test sur ton projet (dogfooding)

```bash
# Étape 1 : Indexer codelens-v2 lui-même (dogfooding)
codelens index ./

# Étape 2 : Vérifier l'index
codelens stats

# Étape 3 : Test de recherche CLI
codelens search "MCP tool handler"
codelens search "HNSW index persistence"

# Étape 4 : Lancer le MCP server
codelens serve

# Étape 5 : Configurer Claude Code (mcp_servers dans settings.json)
# Étape 6 : Ouvrir Claude Code sur codelens-v2
# Étape 7 : Demander à Claude de trouver "the embedding client" → doit utiliser search_codebase
# Étape 8 : Vérifier les logs → zéro grep, zéro Read full-file
```

---

## 12. Installation et configuration

### Prérequis

```bash
# Go 1.22+
go version

# Ollama avec nomic-embed-text
ollama pull nomic-embed-text

# Vérifier
curl http://localhost:11434/api/embeddings -d '{"model":"nomic-embed-text","prompt":"test"}'
```

### Build

```bash
# Compiler les deux binaires
make build

# Ou manuellement
go build -o bin/codelens ./cmd/codelens
go build -o bin/codelens-hook ./cmd/hook

# Installer dans PATH (symlinks ou copier dans /usr/local/bin)
make install
```

### Configuration Claude Code

```bash
# Copier les configs
cp .claude/settings.json ~/.claude/settings.json   # Global, ou garder par projet

# Ajouter le MCP server dans la config Claude Code
# ~/.claude/claude.json ou via: claude mcp add
```

```json
// Ajouter dans ~/.claude/claude.json → mcpServers
{
  "mcpServers": {
    "codelens": {
      "command": "codelens",
      "args": ["serve"],
      "env": {
        "CODELENS_PROJECT": "/path/to/your/project",
        "CODELENS_OLLAMA_URL": "http://localhost:11434",
        "CODELENS_OLLAMA_MODEL": "nomic-embed-text"
      }
    }
  }
}
```

---

## 13. Roadmap

### v0.1 — Foundation (Semaine 1)
- [ ] Setup Go module + cobra CLI
- [ ] Ollama embeddings client
- [ ] SQLite schema + CRUD
- [ ] Chunker générique (pas de tree-sitter encore)
- [ ] HNSW index basique
- [ ] MCP server avec `search_codebase` uniquement
- [ ] Tests unitaires chunker + vector

### v0.2 — Memory (Semaine 2)
- [ ] `remember` + `recall` + JIT verifier
- [ ] `read_file_smart`
- [ ] `index_status`
- [ ] fsnotify watcher (indexation incrémentale)
- [ ] Tests d'intégration memory

### v0.3 — Hook Interceptor (Semaine 3)
- [ ] Binary `codelens-hook`
- [ ] Interception Bash (grep/rg/find)
- [ ] Interception Read (large files)
- [ ] Tests hooks
- [ ] CLAUDE.md finalisé

### v0.4 — Tree-sitter + Benchmarks (Semaine 4)
- [ ] Chunker PHP tree-sitter
- [ ] Chunker TypeScript tree-sitter
- [ ] Benchmarks token savings
- [ ] Test complet sur projet réel
- [ ] README + doc d'installation

### v1.0 — Polish
- [ ] Config YAML (viper)
- [ ] Daemon mode (autostart)
- [ ] Auto-expiration des mémoires
- [ ] Support Go/Python tree-sitter
- [ ] Makefile + install script
