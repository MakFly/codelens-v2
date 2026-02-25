package indexer

import (
	"regexp"
	"strings"
)

var (
	pyClassStart = regexp.MustCompile(`^(\s*)class\s+(\w+)`)
	pyFuncStart  = regexp.MustCompile(`^(\s*)(?:async\s+)?def\s+(\w+)`)
)

func chunkPython(filePath string, lines []string) []Chunk {
	var boundaries []symbolBoundary

	type frame struct {
		start       int
		symbol      string
		kind        string
		indentLevel int
		className   string // non-empty if this is a method inside a class
	}
	var stack []frame

	inString := false
	var stringDelim string

	for i, raw := range lines {
		line := strings.TrimRight(raw, "\r")

		// Handle multi-line strings (""" or ''')
		if inString {
			if strings.Contains(line, stringDelim) {
				inString = false
			}
			continue
		}

		// Check for multi-line string start (not already handled)
		stripped := strings.TrimSpace(line)
		for _, delim := range []string{`"""`, `'''`} {
			count := strings.Count(line, delim)
			if count == 1 {
				inString = true
				stringDelim = delim
				break
			}
		}
		if inString {
			continue
		}

		// Skip empty lines and comments for indent logic
		if stripped == "" || strings.HasPrefix(stripped, "#") {
			continue
		}

		// Skip line continuations
		if strings.HasSuffix(stripped, `\`) {
			continue
		}

		indent := indentLevel(line)

		// Pop stack: non-empty line with indent <= symbol's indent means symbol ended
		for len(stack) > 0 && indent <= stack[len(stack)-1].indentLevel {
			top := stack[len(stack)-1]
			boundaries = append(boundaries, symbolBoundary{top.start, i - 1, top.symbol, top.kind})
			stack = stack[:len(stack)-1]
		}

		// Detect class
		if m := pyClassStart.FindStringSubmatch(line); m != nil {
			start := scanDecorators(lines, i)
			stack = append(stack, frame{start, m[2], "class", indent, ""})
			continue
		}

		// Detect function/method
		if m := pyFuncStart.FindStringSubmatch(line); m != nil {
			start := scanDecorators(lines, i)
			name := m[2]
			kind := "function"

			// If inside a class (any frame on stack is a class), it's a method
			for j := len(stack) - 1; j >= 0; j-- {
				if stack[j].kind == "class" {
					kind = "method"
					name = stack[j].symbol + "." + name
					break
				}
			}

			stack = append(stack, frame{start, name, kind, indent, ""})
		}
	}

	// Close remaining open symbols
	for _, f := range stack {
		boundaries = append(boundaries, symbolBoundary{f.start, len(lines) - 1, f.symbol, f.kind})
	}

	if len(boundaries) == 0 {
		return nil
	}

	return chunkBySymbol(filePath, lines, "python", boundaries)
}

// indentLevel returns the number of leading spaces (tab = 4 spaces).
func indentLevel(line string) int {
	n := 0
	for _, ch := range line {
		if ch == ' ' {
			n++
		} else if ch == '\t' {
			n += 4
		} else {
			break
		}
	}
	return n
}

// scanDecorators scans backwards from line i to include preceding @ decorator lines.
func scanDecorators(lines []string, i int) int {
	start := i
	for j := i - 1; j >= 0; j-- {
		trimmed := strings.TrimSpace(lines[j])
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "@") {
			start = j
		} else {
			break
		}
	}
	return start
}
