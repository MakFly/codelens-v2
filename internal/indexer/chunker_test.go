package indexer

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- PHP Tests ---

func TestChunkPHP_ClassWithMethods(t *testing.T) {
	src := `<?php
namespace App\Services;

class AuthService
{
    private UserRepository $users;

    public function __construct(UserRepository $users)
    {
        $this->users = $users;
    }

    public function login(string $email, string $password): Token
    {
        $user = $this->users->findByEmail($email);
        if (!$user || !password_verify($password, $user->password)) {
            throw new AuthException('Invalid credentials');
        }
        return $this->generateToken($user);
    }

    private function generateToken(User $user): Token
    {
        return new Token(bin2hex(random_bytes(32)), $user->id);
    }
}
`
	lines := strings.Split(src, "\n")
	chunks := chunkPHP("AuthService.php", lines)

	require.Greater(t, len(chunks), 0, "should produce at least one chunk")

	// Find the login method chunk
	var loginChunk *Chunk
	for i := range chunks {
		if strings.Contains(chunks[i].Symbol, "login") {
			loginChunk = &chunks[i]
			break
		}
	}
	require.NotNil(t, loginChunk, "should detect login method")
	assert.Equal(t, "method", loginChunk.SymbolKind)
	assert.Contains(t, loginChunk.Symbol, "AuthService::login")
	assert.Contains(t, loginChunk.Content, "password_verify")
}

func TestChunkPHP_Interface(t *testing.T) {
	src := `<?php
interface UserRepositoryInterface
{
    public function findByEmail(string $email): ?User;
    public function save(User $user): void;
}
`
	lines := strings.Split(src, "\n")
	chunks := chunkPHP("UserRepositoryInterface.php", lines)

	require.Greater(t, len(chunks), 0)
	assert.Equal(t, "interface", chunks[0].SymbolKind)
	assert.Equal(t, "UserRepositoryInterface", chunks[0].Symbol)
}

func TestChunkPHP_StandaloneFunction(t *testing.T) {
	src := `<?php

function formatCurrency(float $amount, string $currency = 'EUR'): string
{
    return number_format($amount, 2) . ' ' . $currency;
}

function validateEmail(string $email): bool
{
    return filter_var($email, FILTER_VALIDATE_EMAIL) !== false;
}
`
	lines := strings.Split(src, "\n")
	chunks := chunkPHP("helpers.php", lines)

	require.GreaterOrEqual(t, len(chunks), 2, "should detect at least 2 standalone functions")

	symbols := make([]string, len(chunks))
	for i, c := range chunks {
		symbols[i] = c.Symbol
	}
	assert.Contains(t, symbols, "formatCurrency")
	assert.Contains(t, symbols, "validateEmail")
}

// --- TypeScript Tests ---

func TestChunkTS_ClassWithMethods(t *testing.T) {
	src := `import { Injectable } from '@nestjs/common';

export class UserService {
    constructor(private readonly userRepo: UserRepository) {}

    async findById(id: string): Promise<User> {
        const user = await this.userRepo.findOne({ where: { id } });
        if (!user) throw new NotFoundException('User not found');
        return user;
    }

    async updateProfile(id: string, dto: UpdateProfileDto): Promise<User> {
        await this.userRepo.update(id, dto);
        return this.findById(id);
    }
}
`
	lines := strings.Split(src, "\n")
	chunks := chunkTypeScript("user.service.ts", lines)

	require.Greater(t, len(chunks), 0, "should produce chunks for TS class")
}

func TestChunkTS_ArrowFunction(t *testing.T) {
	src := `export const calculateDiscount = (price: number, rate: number): number => {
    if (rate < 0 || rate > 1) {
        throw new Error('Rate must be between 0 and 1');
    }
    return price * (1 - rate);
};

export const formatPrice = (price: number, currency = 'EUR'): string => {
    return new Intl.NumberFormat('fr-FR', { style: 'currency', currency }).format(price);
};
`
	lines := strings.Split(src, "\n")
	chunks := chunkTypeScript("pricing.ts", lines)

	require.Greater(t, len(chunks), 0)
}

// --- Go Tests ---

func TestChunkGo_Functions(t *testing.T) {
	src := `package auth

import "errors"

type TokenService struct {
    secret []byte
}

func NewTokenService(secret string) *TokenService {
    return &TokenService{secret: []byte(secret)}
}

func (s *TokenService) Generate(userID string) (string, error) {
    if userID == "" {
        return "", errors.New("userID cannot be empty")
    }
    // ... token generation logic
    return "token_" + userID, nil
}

func (s *TokenService) Validate(token string) (string, error) {
    // ... validation logic
    return "", nil
}
`
	lines := strings.Split(src, "\n")
	chunks := chunkGo("token_service.go", lines)

	require.Greater(t, len(chunks), 0)

	var symbolKinds []string
	for _, c := range chunks {
		symbolKinds = append(symbolKinds, c.SymbolKind)
	}
	assert.Contains(t, symbolKinds, "method", "should detect methods")
}

// --- Generic chunker Tests ---

func TestChunkGeneric_LargeFile(t *testing.T) {
	// Build a 500-line file
	var lines []string
	for i := 0; i < 500; i++ {
		lines = append(lines, "line content here")
	}

	chunks := chunkGeneric("large.txt", lines, "text")

	require.Greater(t, len(chunks), 2, "large file should produce multiple chunks")

	// Verify no chunk exceeds ChunkMaxLines
	for _, c := range chunks {
		lineCount := strings.Count(c.Content, "\n") + 1
		assert.LessOrEqual(t, lineCount, ChunkMaxLines+1, "chunk too large")
	}
}

func TestChunkGeneric_SmallFile(t *testing.T) {
	lines := []string{"func main() {", "    fmt.Println(\"hello\")", "}"}
	chunks := chunkGeneric("small.go", lines, "go")

	require.Equal(t, 1, len(chunks), "small file should be a single chunk")
	assert.Equal(t, 1, chunks[0].StartLine)
}

// --- Python Tests ---

func TestChunkPython_ClassWithMethods(t *testing.T) {
	src := `import hashlib

class AuthService:
    def __init__(self, user_repo):
        self.user_repo = user_repo

    def login(self, email, password):
        user = self.user_repo.find_by_email(email)
        if not user or not user.verify_password(password):
            raise AuthError("Invalid credentials")
        return self.generate_token(user)

    async def fetch(self, url):
        async with aiohttp.ClientSession() as session:
            async with session.get(url) as response:
                return await response.json()
`
	lines := strings.Split(src, "\n")
	chunks := chunkPython("auth_service.py", lines)

	require.Greater(t, len(chunks), 0, "should produce chunks")

	var loginChunk *Chunk
	for i := range chunks {
		if strings.Contains(chunks[i].Symbol, "login") {
			loginChunk = &chunks[i]
			break
		}
	}
	require.NotNil(t, loginChunk, "should detect login method")
	assert.Equal(t, "method", loginChunk.SymbolKind)
	assert.Contains(t, loginChunk.Symbol, "AuthService.login")
}

func TestChunkPython_TopLevelDef(t *testing.T) {
	src := `@app.route("/login")
@require_auth
def login_handler(request):
    email = request.json["email"]
    return authenticate(email)

def helper():
    return 42
`
	lines := strings.Split(src, "\n")
	chunks := chunkPython("routes.py", lines)

	require.Greater(t, len(chunks), 0)

	symbols := make([]string, len(chunks))
	for i, c := range chunks {
		symbols[i] = c.Symbol
	}
	assert.Contains(t, symbols, "login_handler")
	assert.Contains(t, symbols, "helper")

	// Verify decorator is included
	for _, c := range chunks {
		if c.Symbol == "login_handler" {
			assert.Equal(t, "function", c.SymbolKind)
			assert.Contains(t, c.Content, "@app.route")
		}
	}
}

func TestChunkPython_NestedClass(t *testing.T) {
	src := `class Outer:
    class Inner:
        def method(self):
            pass

    def outer_method(self):
        pass
`
	lines := strings.Split(src, "\n")
	chunks := chunkPython("nested.py", lines)
	require.Greater(t, len(chunks), 0)
}

// --- Java Tests ---

func TestChunkJava_ClassWithMethods(t *testing.T) {
	src := `package com.example;

@Service
public class UserService {
    @Autowired
    private UserRepository userRepo;

    @Transactional
    public User findById(String id) {
        return userRepo.findById(id)
            .orElseThrow(() -> new NotFoundException("User not found"));
    }

    public User save(User user) {
        return userRepo.save(user);
    }
}
`
	lines := strings.Split(src, "\n")
	chunks := chunkJava("UserService.java", lines)

	require.Greater(t, len(chunks), 0)

	var findChunk *Chunk
	for i := range chunks {
		if strings.Contains(chunks[i].Symbol, "findById") {
			findChunk = &chunks[i]
			break
		}
	}
	require.NotNil(t, findChunk, "should detect findById method")
	assert.Equal(t, "method", findChunk.SymbolKind)
	assert.Contains(t, findChunk.Symbol, "UserService.findById")
	// Annotation should be included
	assert.Contains(t, findChunk.Content, "@Transactional")
}

func TestChunkJava_Interface(t *testing.T) {
	src := `public interface UserRepository {
    User findByEmail(String email);

    default User findOrCreate(String email) {
        User user = findByEmail(email);
        return user != null ? user : new User(email);
    }
}
`
	lines := strings.Split(src, "\n")
	chunks := chunkJava("UserRepository.java", lines)

	require.Greater(t, len(chunks), 0)
	// Should find the interface
	found := false
	for _, c := range chunks {
		if c.SymbolKind == "interface" {
			found = true
			assert.Equal(t, "UserRepository", c.Symbol)
		}
	}
	assert.True(t, found, "should detect interface")
}

func TestChunkJava_Enum(t *testing.T) {
	src := `public enum Status {
    ACTIVE,
    INACTIVE,
    PENDING;

    public boolean isActive() {
        return this == ACTIVE;
    }
}
`
	lines := strings.Split(src, "\n")
	chunks := chunkJava("Status.java", lines)

	require.Greater(t, len(chunks), 0)
	found := false
	for _, c := range chunks {
		if c.SymbolKind == "enum" {
			found = true
			assert.Equal(t, "Status", c.Symbol)
		}
	}
	assert.True(t, found, "should detect enum")
}

// --- Rust Tests ---

func TestChunkRust_ImplWithMethods(t *testing.T) {
	src := `use std::net::TcpListener;

pub struct Server {
    listener: TcpListener,
}

impl Server {
    pub fn new(addr: &str) -> Self {
        let listener = TcpListener::bind(addr).unwrap();
        Server { listener }
    }

    pub async fn handle(&self, req: Request) -> Response {
        match req.method {
            Method::GET => self.handle_get(req),
            Method::POST => self.handle_post(req),
            _ => Response::not_found(),
        }
    }
}
`
	lines := strings.Split(src, "\n")
	chunks := chunkRust("server.rs", lines)

	require.Greater(t, len(chunks), 0)

	var handleChunk *Chunk
	for i := range chunks {
		if strings.Contains(chunks[i].Symbol, "handle") && chunks[i].SymbolKind == "method" {
			handleChunk = &chunks[i]
			break
		}
	}
	require.NotNil(t, handleChunk, "should detect handle method")
	assert.Contains(t, handleChunk.Symbol, "Server.handle")
}

func TestChunkRust_TraitDef(t *testing.T) {
	src := `pub trait Handler {
    fn handle(&self, req: Request) -> Response;

    fn default_response(&self) -> Response {
        Response::ok()
    }
}
`
	lines := strings.Split(src, "\n")
	chunks := chunkRust("handler.rs", lines)

	require.Greater(t, len(chunks), 0)
	found := false
	for _, c := range chunks {
		if c.SymbolKind == "trait" {
			found = true
			assert.Equal(t, "Handler", c.Symbol)
		}
	}
	assert.True(t, found, "should detect trait")
}

func TestChunkRust_TopLevelFn(t *testing.T) {
	src := `pub fn main() {
    let server = Server::new("127.0.0.1:8080");
    server.run();
}

fn helper() -> u32 {
    42
}
`
	lines := strings.Split(src, "\n")
	chunks := chunkRust("main.rs", lines)

	require.Greater(t, len(chunks), 0)
	symbols := make([]string, len(chunks))
	for i, c := range chunks {
		symbols[i] = c.Symbol
	}
	assert.Contains(t, symbols, "main")
	assert.Contains(t, symbols, "helper")
}

// --- Ruby Tests ---

func TestChunkRuby_ClassWithMethods(t *testing.T) {
	src := `class User < ApplicationRecord
  validates :email, presence: true

  def initialize(attrs = {})
    @email = attrs[:email]
    @name = attrs[:name]
  end

  def save!
    validate!
    persist
  end
end
`
	lines := strings.Split(src, "\n")
	chunks := chunkRuby("user.rb", lines)

	require.Greater(t, len(chunks), 0)

	var saveChunk *Chunk
	for i := range chunks {
		if strings.Contains(chunks[i].Symbol, "save!") {
			saveChunk = &chunks[i]
			break
		}
	}
	require.NotNil(t, saveChunk, "should detect save! method")
	assert.Equal(t, "method", saveChunk.SymbolKind)
	assert.Contains(t, saveChunk.Symbol, "User.save!")
}

func TestChunkRuby_Module(t *testing.T) {
	src := `module Authentication
  def authenticate(email, password)
    user = find_user(email)
    verify_password(user, password)
  end

  def generate_token(user)
    JWT.encode({ user_id: user.id }, secret)
  end
end
`
	lines := strings.Split(src, "\n")
	chunks := chunkRuby("auth.rb", lines)

	require.Greater(t, len(chunks), 0)
	found := false
	for _, c := range chunks {
		if c.SymbolKind == "module" {
			found = true
			assert.Equal(t, "Authentication", c.Symbol)
		}
	}
	assert.True(t, found, "should detect module")
}

func TestChunkRuby_PostfixIf(t *testing.T) {
	src := `class Guard
  def check(user)
    return false if user.nil?
    return false unless user.active?
    true
  end
end
`
	lines := strings.Split(src, "\n")
	chunks := chunkRuby("guard.rb", lines)

	require.Greater(t, len(chunks), 0)
	// Postfix if/unless should not break depth tracking
	found := false
	for _, c := range chunks {
		if strings.Contains(c.Symbol, "check") {
			found = true
			assert.Equal(t, "method", c.SymbolKind)
		}
	}
	assert.True(t, found, "should detect check method with postfix conditionals")
}

// --- C# Tests ---

func TestChunkCSharp_ClassWithMethods(t *testing.T) {
	src := `using System.Threading.Tasks;

namespace MyApp.Services
{
    public class UserService
    {
        private readonly IUserRepository _repo;

        public UserService(IUserRepository repo)
        {
            _repo = repo;
        }

        public async Task<User> GetByIdAsync(string id)
        {
            var user = await _repo.FindAsync(id);
            if (user == null)
                throw new NotFoundException("User not found");
            return user;
        }

        public async Task<User> UpdateAsync(string id, UpdateDto dto)
        {
            await _repo.UpdateAsync(id, dto);
            return await GetByIdAsync(id);
        }
    }
}
`
	lines := strings.Split(src, "\n")
	chunks := chunkCSharp("UserService.cs", lines)

	require.Greater(t, len(chunks), 0)

	var getChunk *Chunk
	for i := range chunks {
		if strings.Contains(chunks[i].Symbol, "GetByIdAsync") {
			getChunk = &chunks[i]
			break
		}
	}
	require.NotNil(t, getChunk, "should detect GetByIdAsync method")
	assert.Equal(t, "method", getChunk.SymbolKind)
	assert.Contains(t, getChunk.Symbol, "UserService.GetByIdAsync")
}

func TestChunkCSharp_Interface(t *testing.T) {
	src := `namespace MyApp.Contracts
{
    public interface IUserRepository
    {
        Task<User> FindAsync(string id);
        Task UpdateAsync(string id, UpdateDto dto);
    }
}
`
	lines := strings.Split(src, "\n")
	chunks := chunkCSharp("IUserRepository.cs", lines)

	require.Greater(t, len(chunks), 0)
	found := false
	for _, c := range chunks {
		if c.SymbolKind == "interface" {
			found = true
			assert.Equal(t, "IUserRepository", c.Symbol)
		}
	}
	assert.True(t, found, "should detect interface")
}

// --- Vue/Svelte Script Extraction Tests ---

func TestExtractScriptSection(t *testing.T) {
	src := `<template>
  <div>{{ message }}</div>
</template>

<script setup lang="ts">
import { ref } from 'vue'

const message = ref('Hello')

function greet(name: string): string {
    return message.value + ' ' + name
}
</script>

<style scoped>
.container { color: red; }
</style>`
	lines := strings.Split(src, "\n")
	scriptLines, offset := extractScriptSection(lines)

	require.Greater(t, len(scriptLines), 0, "should extract script section")
	assert.Greater(t, offset, 0, "offset should be > 0")
	assert.Contains(t, strings.Join(scriptLines, "\n"), "ref('Hello')")
	assert.NotContains(t, strings.Join(scriptLines, "\n"), "<template>")
	assert.NotContains(t, strings.Join(scriptLines, "\n"), "<style")
}

// --- languageFromPath Tests ---

func TestLanguageFromPath(t *testing.T) {
	tests := []struct{ path, want string }{
		{"main.go", "go"},
		{"app.py", "python"},
		{"App.java", "java"},
		{"lib.rs", "rust"},
		{"model.rb", "ruby"},
		{"Form.cs", "csharp"},
		{"App.vue", "vue"},
		{"Page.svelte", "svelte"},
		{"schema.sql", "sql"},
		{"config.yaml", "yaml"},
		{"config.yml", "yaml"},
		{"Dockerfile", "dockerfile"},
		{"Dockerfile.prod", "dockerfile"},
		{"Makefile", "makefile"},
		{"styles.css", "css"},
		{"app.tsx", "typescript"},
		{"index.mjs", "typescript"},
		{"lib.cjs", "typescript"},
		{"types.pyi", "python"},
		{"Gemfile.gemspec", "ruby"},
		{"test.rake", "ruby"},
		{"unknown.xyz", ""},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, languageFromPath(tt.path), tt.path)
	}
}

// --- Exclusion Tests ---

func TestShouldSkipDir_Nested(t *testing.T) {
	assert.True(t, shouldSkipDir("packages/api/node_modules"))
	assert.True(t, shouldSkipDir("src/utils/__pycache__"))
	assert.True(t, shouldSkipDir("apps/web/.next"))
	assert.True(t, shouldSkipDir("packages/ui/dist"))
	assert.True(t, shouldSkipDir("apps/api/vendor"))
	assert.False(t, shouldSkipDir("src/utils"))
	assert.False(t, shouldSkipDir("packages/api"))
}

func TestShouldSkipFile_LockFiles(t *testing.T) {
	assert.True(t, shouldSkipFile("package-lock.json"))
	assert.True(t, shouldSkipFile("yarn.lock"))
	assert.True(t, shouldSkipFile("pnpm-lock.yaml"))
	assert.True(t, shouldSkipFile("composer.lock"))
	assert.True(t, shouldSkipFile("bun.lockb"))
	assert.False(t, shouldSkipFile("Cargo.lock"))
	assert.False(t, shouldSkipFile("src/lock.go"))
}

func TestShouldSkipFile_SensitiveFiles(t *testing.T) {
	assert.True(t, shouldSkipFile(".env"))
	assert.True(t, shouldSkipFile(".env.local"))
	assert.True(t, shouldSkipFile(".env.production"))
	assert.True(t, shouldSkipFile("server.key"))
	assert.True(t, shouldSkipFile("cert.pem"))
	assert.True(t, shouldSkipFile("id_rsa"))
	assert.True(t, shouldSkipFile("credentials.json"))
	assert.True(t, shouldSkipFile("secrets.json"))
	assert.False(t, shouldSkipFile("config.json"))
	assert.False(t, shouldSkipFile("environment.ts"))
	assert.False(t, shouldSkipFile("id_rsa.pub"))
}

func TestShouldSkipFile_BinaryAndMedia(t *testing.T) {
	assert.True(t, shouldSkipFile("logo.png"))
	assert.True(t, shouldSkipFile("image.jpg"))
	assert.True(t, shouldSkipFile("font.woff2"))
	assert.True(t, shouldSkipFile("archive.zip"))
	assert.True(t, shouldSkipFile("compiled.pyc"))
	assert.True(t, shouldSkipFile("Main.class"))
	assert.True(t, shouldSkipFile("lib.so"))
	assert.True(t, shouldSkipFile("data.sqlite"))
	assert.False(t, shouldSkipFile("main.go"))
	assert.False(t, shouldSkipFile("app.ts"))
}

// --- ChunkFile router Tests ---

func TestChunkFile_RoutesToCorrectChunker(t *testing.T) {
	phpSrc := "<?php\nclass Foo {\n    public function bar() { return 1; }\n}\n"
	chunks := ChunkFile("test.php", phpSrc, "php")
	assert.Greater(t, len(chunks), 0)
	for _, c := range chunks {
		assert.Equal(t, "php", c.Language)
		assert.NotEmpty(t, c.ID)
		assert.NotEmpty(t, c.Hash, "chunk should have a hash after ChunkFile (set by indexer)")
	}
}
