package indexer

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// IgnoreList holds parsed patterns from a .codelensignore file.
type IgnoreList struct {
	patterns []ignorePattern
}

type ignorePattern struct {
	original string
	negated  bool   // !pattern
	dirOnly  bool   // pattern ends with /
	hasSlash bool   // pattern contains / (match full path)
	glob     string // cleaned pattern for matching
}

// loadIgnorePatterns reads and parses a .codelensignore file from the project root.
// Returns (nil, nil) if the file does not exist.
func loadIgnorePatterns(projectRoot string) (*IgnoreList, error) {
	path := filepath.Join(projectRoot, ".codelensignore")
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var patterns []ignorePattern
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		p := ignorePattern{original: line}

		if strings.HasPrefix(line, "!") {
			p.negated = true
			line = line[1:]
		}

		if strings.HasSuffix(line, "/") {
			p.dirOnly = true
			line = strings.TrimSuffix(line, "/")
		}

		p.hasSlash = strings.Contains(line, "/")
		p.glob = line

		patterns = append(patterns, p)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return &IgnoreList{patterns: patterns}, nil
}

// IsIgnored returns true if the given relative path should be ignored.
// relPath must use forward slashes. isDir indicates whether the path is a directory.
func (il *IgnoreList) IsIgnored(relPath string, isDir bool) bool {
	if il == nil || len(il.patterns) == 0 {
		return false
	}

	ignored := false
	for _, p := range il.patterns {
		if p.dirOnly && !isDir {
			continue
		}

		matched := matchPattern(p.glob, p.hasSlash, relPath)
		if matched {
			ignored = !p.negated
		}
	}
	return ignored
}

// matchPattern matches a single glob pattern against a path.
func matchPattern(pattern string, hasSlash bool, path string) bool {
	if !hasSlash {
		// No slash in pattern → match against basename only
		base := filepath.Base(path)
		ok, _ := filepath.Match(pattern, base)
		return ok
	}

	// Handle ** patterns
	if strings.Contains(pattern, "**") {
		return matchDoublestar(pattern, path)
	}

	// Simple path pattern — direct match
	ok, _ := filepath.Match(pattern, path)
	return ok
}

// matchDoublestar handles patterns containing **.
func matchDoublestar(pattern, path string) bool {
	// Split pattern on **
	parts := strings.Split(pattern, "**")

	if len(parts) == 2 {
		prefix := strings.TrimSuffix(parts[0], "/")
		suffix := strings.TrimPrefix(parts[1], "/")

		// Pattern like **/suffix — match suffix against any subpath
		if prefix == "" {
			return matchSuffix(suffix, path)
		}

		// Pattern like prefix/** — match if path starts with prefix
		if suffix == "" {
			return strings.HasPrefix(path, prefix+"/") || path == prefix
		}

		// Pattern like prefix/**/suffix
		if !strings.HasPrefix(path, prefix+"/") {
			return false
		}
		rest := path[len(prefix)+1:]
		return matchSuffix(suffix, rest)
	}

	// Multiple ** segments (e.g. **/middle/**)
	// Check that each non-empty segment between ** appears in order in the path
	if len(parts) > 2 {
		// Extract the middle segments
		segments := make([]string, 0, len(parts))
		for _, p := range parts {
			s := strings.Trim(p, "/")
			if s != "" {
				segments = append(segments, s)
			}
		}
		// All segments must appear in order within path components
		pathParts := strings.Split(path, "/")
		pi := 0
		for _, seg := range segments {
			found := false
			for pi < len(pathParts) {
				ok, _ := filepath.Match(seg, pathParts[pi])
				if ok {
					pi++
					found = true
					break
				}
				pi++
			}
			if !found {
				return false
			}
		}
		return true
	}

	return false
}

// matchSuffix checks if any subpath of path matches the suffix pattern.
func matchSuffix(suffix, path string) bool {
	if suffix == "" {
		return true
	}

	// Try matching against the full path and every subpath
	candidates := []string{path}
	for i, ch := range path {
		if ch == '/' && i+1 < len(path) {
			candidates = append(candidates, path[i+1:])
		}
	}

	for _, c := range candidates {
		ok, _ := filepath.Match(suffix, c)
		if ok {
			return true
		}
	}
	return false
}
