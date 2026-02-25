package indexer

import (
	"regexp"
	"strings"
)

var (
	csClassStart  = regexp.MustCompile(`^\s*(?:public\s+|private\s+|protected\s+|internal\s+)?(?:abstract\s+|sealed\s+|static\s+|partial\s+)*(?:class|interface|struct|enum|record)\s+(\w+)`)
	csMethodStart = regexp.MustCompile(`^\s+(?:(?:public|private|protected|internal|static|virtual|override|abstract|async|partial)\s+)*(?:[\w<>\[\]?]+)\s+(\w+)\s*\(`)
	csNamespace   = regexp.MustCompile(`^\s*namespace\s+`)
)

func chunkCSharp(filePath string, lines []string) []Chunk {
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

		// Namespaces: detected but not tracked as symbols
		if csNamespace.MatchString(line) {
			continue
		}

		// Classes, interfaces, structs, enums, records at depth <= 1 (could be inside namespace)
		if m := csClassStart.FindStringSubmatch(line); m != nil && prevDepth <= 1 {
			kind := "class"
			switch {
			case strings.Contains(line, "interface"):
				kind = "interface"
			case strings.Contains(line, "struct"):
				kind = "struct"
			case strings.Contains(line, "enum"):
				kind = "enum"
			}
			currentClass = m[1]
			start := scanBackAttributes(lines, i)
			stack = append(stack, frame{start, m[1], kind, prevDepth + 1, true})
			continue
		}

		// Methods: inside a class (prevDepth == class depth + 1, so class depth + 1)
		if m := csMethodStart.FindStringSubmatch(line); m != nil && currentClass != "" && prevDepth >= 1 {
			// Only detect methods one level inside the current class
			classDepth := -1
			for j := len(stack) - 1; j >= 0; j-- {
				if stack[j].isType {
					classDepth = stack[j].depth
					break
				}
			}
			if classDepth >= 0 && prevDepth == classDepth {
				symbol := currentClass + "." + m[1]
				start := scanBackAttributes(lines, i)
				stack = append(stack, frame{start, symbol, "method", prevDepth + 1, false})
				continue
			}
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

	return chunkBySymbol(filePath, lines, "csharp", boundaries)
}

// scanBackAttributes walks backwards from line idx to include preceding C# attribute lines.
func scanBackAttributes(lines []string, idx int) int {
	start := idx
	for j := idx - 1; j >= 0; j-- {
		trimmed := strings.TrimSpace(lines[j])
		if strings.HasPrefix(trimmed, "[") {
			start = j
		} else {
			break
		}
	}
	return start
}
