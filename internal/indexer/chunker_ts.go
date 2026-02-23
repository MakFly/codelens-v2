package indexer

import (
	"regexp"
	"strings"
)

// TypeScript/JavaScript chunker (regex-based, v0.1).
// Detects: class, function, async function, arrow const, export default function.

var (
	tsClassStart    = regexp.MustCompile(`^(?:export\s+)?(?:abstract\s+)?class\s+(\w+)`)
	tsFuncStart     = regexp.MustCompile(`^(?:export\s+)?(?:async\s+)?function\s+(\w+)\s*[\(<]`)
	tsMethodStart   = regexp.MustCompile(`^\s+(?:(?:public|private|protected|static|async|override)\s+)*(\w+)\s*\([^)]*\)\s*(?::\s*\S+\s*)?\{`)
	tsArrowConst    = regexp.MustCompile(`^(?:export\s+)?(?:const|let)\s+(\w+)\s*=\s*(?:async\s+)?\(?.*\)?\s*=>`)
	tsExportDefault = regexp.MustCompile(`^export\s+default\s+(?:async\s+)?function\s*(\w*)`)
)

func chunkTypeScript(filePath string, lines []string) []Chunk {
	var boundaries []symbolBoundary
	depth := 0
	currentClass := ""

	type frame struct {
		start  int
		symbol string
		kind   string
		depth  int
	}
	var stack []frame

	for i, raw := range lines {
		line := strings.TrimRight(raw, "\r")

		// Skip comment lines
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "*") {
			continue
		}

		opens := strings.Count(line, "{")
		closes := strings.Count(line, "}")
		prevDepth := depth
		depth += opens - closes

		// Class
		if m := tsClassStart.FindStringSubmatch(line); m != nil {
			currentClass = m[1]
			stack = append(stack, frame{i, m[1], "class", prevDepth + 1})
			continue
		}

		// Top-level function / export default function
		if m := tsFuncStart.FindStringSubmatch(line); m != nil {
			stack = append(stack, frame{i, m[1], "function", prevDepth + 1})
			continue
		}
		if m := tsExportDefault.FindStringSubmatch(line); m != nil {
			name := m[1]
			if name == "" {
				name = "default"
			}
			stack = append(stack, frame{i, name, "function", prevDepth + 1})
			continue
		}

		// Arrow const at top level (depth 0)
		if depth <= 1 {
			if m := tsArrowConst.FindStringSubmatch(line); m != nil {
				stack = append(stack, frame{i, m[1], "function", prevDepth + 1})
				continue
			}
		}

		// Method inside class
		if currentClass != "" && prevDepth == 1 {
			if m := tsMethodStart.FindStringSubmatch(line); m != nil {
				methodName := currentClass + "." + m[1]
				stack = append(stack, frame{i, methodName, "method", prevDepth + 1})
				continue
			}
		}

		// Pop closed frames
		for len(stack) > 0 && depth < stack[len(stack)-1].depth {
			top := stack[len(stack)-1]
			boundaries = append(boundaries, symbolBoundary{top.start, i, top.symbol, top.kind})
			stack = stack[:len(stack)-1]
			if top.kind == "class" {
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

	return chunkBySymbol(filePath, lines, "typescript", boundaries)
}
