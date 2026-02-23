package jit

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVerify_ValidCitation(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "test.php")

	content := "<?php\nclass Foo {\n    public function bar() {\n        return 42;\n    }\n}\n"
	require.NoError(t, os.WriteFile(filePath, []byte(content), 0644))

	v := New(dir)
	c := Citation{
		FilePath:  "test.php",
		LineStart: 3,
		LineEnd:   5,
	}

	// Compute hash first
	require.NoError(t, v.HashCitation(&c))
	assert.NotEmpty(t, c.Hash)

	// Verify against same content → should be valid
	assert.True(t, v.VerifyCitation(c), "citation should be valid for unchanged content")
}

func TestVerify_ModifiedCode(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "test.php")

	original := "<?php\nclass Foo {\n    public function bar() {\n        return 42;\n    }\n}\n"
	require.NoError(t, os.WriteFile(filePath, []byte(original), 0644))

	v := New(dir)
	c := Citation{FilePath: "test.php", LineStart: 3, LineEnd: 5}
	require.NoError(t, v.HashCitation(&c))

	// Modify the cited lines
	modified := "<?php\nclass Foo {\n    public function bar() {\n        return 99; // changed!\n    }\n}\n"
	require.NoError(t, os.WriteFile(filePath, []byte(modified), 0644))

	// Verify → should fail because code changed
	assert.False(t, v.VerifyCitation(c), "citation should be invalid after code modification")
}

func TestVerify_DeletedFile(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "deleted.php")

	content := "<?php\nfunction gone() {}\n"
	require.NoError(t, os.WriteFile(filePath, []byte(content), 0644))

	v := New(dir)
	c := Citation{FilePath: "deleted.php", LineStart: 2, LineEnd: 2}
	require.NoError(t, v.HashCitation(&c))

	// Delete the file
	require.NoError(t, os.Remove(filePath))

	// Verify → should fail (file gone)
	assert.False(t, v.VerifyCitation(c), "citation should be invalid for deleted file")
}

func TestVerify_VerifyAll_AllValid(t *testing.T) {
	dir := t.TempDir()

	write := func(name, content string) {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0644))
	}

	write("a.php", "<?php\nfunction a() { return 1; }\n")
	write("b.php", "<?php\nfunction b() { return 2; }\n")

	v := New(dir)
	c1 := Citation{FilePath: "a.php", LineStart: 2, LineEnd: 2}
	c2 := Citation{FilePath: "b.php", LineStart: 2, LineEnd: 2}
	require.NoError(t, v.HashCitation(&c1))
	require.NoError(t, v.HashCitation(&c2))

	assert.True(t, v.VerifyAll([]Citation{c1, c2}), "all citations should be valid")
}

func TestVerify_VerifyAll_PartialInvalid(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.php"),
		[]byte("<?php\nfunction a() { return 1; }\n"), 0644))

	v := New(dir)
	c1 := Citation{FilePath: "a.php", LineStart: 2, LineEnd: 2}
	require.NoError(t, v.HashCitation(&c1))

	// c2 points to non-existent file
	c2 := Citation{FilePath: "nonexistent.php", LineStart: 1, LineEnd: 1, Hash: "fakehash"}

	assert.False(t, v.VerifyAll([]Citation{c1, c2}), "should fail if any citation is invalid")
}

func TestHashContent_Deterministic(t *testing.T) {
	h1 := HashContent("hello world")
	h2 := HashContent("hello world")
	assert.Equal(t, h1, h2, "same content should produce same hash")

	h3 := HashContent("different content")
	assert.NotEqual(t, h1, h3, "different content should produce different hash")
}

func TestLineCount(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "test.txt")

	content := "line1\nline2\nline3\nline4\nline5"
	require.NoError(t, os.WriteFile(filePath, []byte(content), 0644))

	count, err := LineCount(filePath)
	require.NoError(t, err)
	assert.Equal(t, 5, count)
}
