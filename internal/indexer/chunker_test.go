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
