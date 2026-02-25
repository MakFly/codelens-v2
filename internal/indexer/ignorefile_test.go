package indexer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadIgnorePatterns_FileNotFound(t *testing.T) {
	il, err := loadIgnorePatterns(t.TempDir())
	assert.NoError(t, err)
	assert.Nil(t, il)
}

func TestLoadIgnorePatterns_BasicPatterns(t *testing.T) {
	dir := t.TempDir()
	content := "# comment\n*.generated.*\nlib/seo/\n!keep.ts\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".codelensignore"), []byte(content), 0644))

	il, err := loadIgnorePatterns(dir)
	require.NoError(t, err)
	require.NotNil(t, il)
	assert.Len(t, il.patterns, 3)

	assert.False(t, il.patterns[0].negated)
	assert.Equal(t, "*.generated.*", il.patterns[0].glob)

	assert.True(t, il.patterns[1].dirOnly)
	assert.Equal(t, "lib/seo", il.patterns[1].glob)

	assert.True(t, il.patterns[2].negated)
	assert.Equal(t, "keep.ts", il.patterns[2].glob)
}

func TestIgnoreList_GlobBasename(t *testing.T) {
	il := &IgnoreList{
		patterns: []ignorePattern{
			{glob: "*.generated.*", hasSlash: false},
		},
	}
	assert.True(t, il.IsIgnored("foo.generated.ts", false))
	assert.True(t, il.IsIgnored("lib/seo/vehicle.generated.js", false))
	assert.False(t, il.IsIgnored("foo.ts", false))
	assert.False(t, il.IsIgnored("generated.ts", false))
}

func TestIgnoreList_DirOnly(t *testing.T) {
	il := &IgnoreList{
		patterns: []ignorePattern{
			{glob: "lib/seo", dirOnly: true, hasSlash: true},
		},
	}
	assert.True(t, il.IsIgnored("lib/seo", true))
	assert.False(t, il.IsIgnored("lib/seo.ts", false))
	assert.False(t, il.IsIgnored("lib/seo/file.ts", false)) // dirOnly matches dirs only
}

func TestIgnoreList_Negation(t *testing.T) {
	il := &IgnoreList{
		patterns: []ignorePattern{
			{glob: "*.log", hasSlash: false},
			{glob: "important.log", hasSlash: false, negated: true},
		},
	}
	assert.True(t, il.IsIgnored("debug.log", false))
	assert.True(t, il.IsIgnored("error.log", false))
	assert.False(t, il.IsIgnored("important.log", false))
}

func TestIgnoreList_PathPattern(t *testing.T) {
	il := &IgnoreList{
		patterns: []ignorePattern{
			{glob: "lib/seo/*.ts", hasSlash: true},
		},
	}
	assert.True(t, il.IsIgnored("lib/seo/foo.ts", false))
	assert.False(t, il.IsIgnored("src/foo.ts", false))
}

func TestIgnoreList_DoubleGlobstar(t *testing.T) {
	il := &IgnoreList{
		patterns: []ignorePattern{
			{glob: "**/generated/**", hasSlash: true},
		},
	}
	assert.True(t, il.IsIgnored("deep/path/generated/foo.ts", false))
	assert.True(t, il.IsIgnored("generated/foo.ts", false))
	assert.False(t, il.IsIgnored("deep/path/foo.ts", false))
}

func TestIgnoreList_DoubleGlobstarPrefix(t *testing.T) {
	il := &IgnoreList{
		patterns: []ignorePattern{
			{glob: "**/*.generated.ts", hasSlash: true},
		},
	}
	assert.True(t, il.IsIgnored("lib/seo/vehicle.generated.ts", false))
	assert.True(t, il.IsIgnored("vehicle.generated.ts", false))
	assert.False(t, il.IsIgnored("vehicle.ts", false))
}

func TestIgnoreList_Comments(t *testing.T) {
	dir := t.TempDir()
	content := "# this is a comment\n\n# another comment\n*.log\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".codelensignore"), []byte(content), 0644))

	il, err := loadIgnorePatterns(dir)
	require.NoError(t, err)
	require.NotNil(t, il)
	assert.Len(t, il.patterns, 1)
	assert.Equal(t, "*.log", il.patterns[0].glob)
}

func TestIgnoreList_Nil(t *testing.T) {
	var il *IgnoreList
	assert.False(t, il.IsIgnored("anything.ts", false))
	assert.False(t, il.IsIgnored("dir", true))
}
