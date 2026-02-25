package indexer

import (
	"regexp"
	"strings"
)

var (
	rubyClassStart  = regexp.MustCompile(`^\s*(class|module)\s+(\w+(?:::\w+)*)`)
	rubyDefStart    = regexp.MustCompile(`^\s*def\s+(?:self\.)?(\w+[?!=]?)`)
	rubyBlockOpen   = regexp.MustCompile(`^\s*(?:class|module|def|do|begin|case)\b`)
	rubyKeywordCond = regexp.MustCompile(`^\s*(?:if|unless|while|until|for)\b`)
	rubyEnd         = regexp.MustCompile(`^\s*end\b`)
)

func chunkRuby(filePath string, lines []string) []Chunk {
	var boundaries []symbolBoundary

	type frame struct {
		start  int
		symbol string
		kind   string
		depth  int
	}
	var stack []frame

	depth := 0
	currentClass := ""

	for i, raw := range lines {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(line)

		// Skip empty lines and comments
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		// Check for end keyword (depth -1)
		if rubyEnd.MatchString(line) {
			depth--

			// Pop stack when depth drops below frame's entry depth
			for len(stack) > 0 && depth < stack[len(stack)-1].depth {
				top := stack[len(stack)-1]
				boundaries = append(boundaries, symbolBoundary{top.start, i, top.symbol, top.kind})
				stack = stack[:len(stack)-1]

				// Clear currentClass if we closed a class/module
				if top.kind == "class" || top.kind == "module" {
					currentClass = ""
				}
			}
			continue
		}

		// Detect class or module
		if m := rubyClassStart.FindStringSubmatch(line); m != nil {
			kind := m[1] // "class" or "module"
			name := m[2]
			stack = append(stack, frame{i, name, kind, depth})
			currentClass = name
			depth++
			continue
		}

		// Detect def
		if m := rubyDefStart.FindStringSubmatch(line); m != nil {
			name := m[1]
			kind := "function"
			if depth > 0 && currentClass != "" {
				kind = "method"
				name = currentClass + "." + name
			}
			stack = append(stack, frame{i, name, kind, depth})
			depth++
			continue
		}

		// Other block-opening keywords
		if rubyBlockOpen.MatchString(line) {
			depth++
			continue
		}

		// Conditional keywords at start of line
		if rubyKeywordCond.MatchString(line) {
			depth++
			continue
		}
	}

	// Close remaining open symbols
	for _, f := range stack {
		boundaries = append(boundaries, symbolBoundary{f.start, len(lines) - 1, f.symbol, f.kind})
	}

	if len(boundaries) == 0 {
		return nil
	}

	return chunkBySymbol(filePath, lines, "ruby", boundaries)
}
