package indexer

import (
	"regexp"
	"strings"
)

var (
	javaClassStart  = regexp.MustCompile(`^\s*(?:public\s+|private\s+|protected\s+)?(?:abstract\s+|final\s+|static\s+)*(?:class|interface|enum|record)\s+(\w+)`)
	javaMethodStart = regexp.MustCompile(`^\s+(?:(?:public|private|protected|static|final|abstract|synchronized|default)\s+)*(?:[\w<>\[\]]+)\s+(\w+)\s*\(`)
)

func chunkJava(filePath string, lines []string) []Chunk {
	var boundaries []symbolBoundary
	depth := 0
	currentClass := ""

	type frame struct {
		start  int
		symbol string
		kind   string
		depth  int
		isType bool
	}
	var stack []frame

	for i, raw := range lines {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") {
			continue
		}

		opens := strings.Count(line, "{")
		closes := strings.Count(line, "}")
		prevDepth := depth
		depth += opens - closes

		// Classes, interfaces, enums, records at depth 0
		if m := javaClassStart.FindStringSubmatch(line); m != nil && prevDepth == 0 {
			kind := "class"
			if strings.Contains(line, "interface") {
				kind = "interface"
			} else if strings.Contains(line, "enum") {
				kind = "enum"
			}
			currentClass = m[1]
			start := scanBackAnnotations(lines, i)
			stack = append(stack, frame{start, m[1], kind, prevDepth + 1, true})
			continue
		}

		// Methods at depth 1 (inside a class body)
		if m := javaMethodStart.FindStringSubmatch(line); m != nil && prevDepth == 1 {
			symbol := m[1]
			if currentClass != "" {
				symbol = currentClass + "." + symbol
			}
			start := scanBackAnnotations(lines, i)
			stack = append(stack, frame{start, symbol, "method", prevDepth + 1, false})
			continue
		}

		// Pop stack when brace depth decreases
		for len(stack) > 0 && depth < stack[len(stack)-1].depth {
			top := stack[len(stack)-1]
			boundaries = append(boundaries, symbolBoundary{top.start, i, top.symbol, top.kind})
			stack = stack[:len(stack)-1]
			if top.isType {
				currentClass = ""
			}
		}
	}

	for _, f := range stack {
		boundaries = append(boundaries, symbolBoundary{f.start, len(lines) - 1, f.symbol, f.kind})
	}

	if len(boundaries) == 0 {
		return nil
	}

	return chunkBySymbol(filePath, lines, "java", boundaries)
}

// scanBackAnnotations walks backwards from line idx to include preceding annotation lines.
func scanBackAnnotations(lines []string, idx int) int {
	start := idx
	for j := idx - 1; j >= 0; j-- {
		trimmed := strings.TrimSpace(lines[j])
		if strings.HasPrefix(trimmed, "@") {
			start = j
		} else {
			break
		}
	}
	return start
}
