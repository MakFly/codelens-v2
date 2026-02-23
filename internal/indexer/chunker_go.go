package indexer

import (
	"regexp"
	"strings"
)

var (
	goFuncStart   = regexp.MustCompile(`^func\s+(?:\(\s*\w+\s+\*?\w+\s*\)\s+)?(\w+)\s*\(`)
	goTypeStart   = regexp.MustCompile(`^type\s+(\w+)\s+(?:struct|interface)`)
)

func chunkGo(filePath string, lines []string) []Chunk {
	var boundaries []symbolBoundary
	depth := 0

	type frame struct {
		start  int
		symbol string
		kind   string
		depth  int
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

		if m := goFuncStart.FindStringSubmatch(line); m != nil && prevDepth == 0 {
			kind := "function"
			// Detect method receiver: func (r *Type) Name(
			if strings.Contains(line[:strings.Index(line, m[1])], ")") {
				kind = "method"
			}
			stack = append(stack, frame{i, m[1], kind, prevDepth + 1})
			continue
		}

		if m := goTypeStart.FindStringSubmatch(line); m != nil && prevDepth == 0 {
			kind := "struct"
			if strings.Contains(line, "interface") {
				kind = "interface"
			}
			stack = append(stack, frame{i, m[1], kind, prevDepth + 1})
			continue
		}

		for len(stack) > 0 && depth < stack[len(stack)-1].depth {
			top := stack[len(stack)-1]
			boundaries = append(boundaries, symbolBoundary{top.start, i, top.symbol, top.kind})
			stack = stack[:len(stack)-1]
		}
	}

	for _, f := range stack {
		boundaries = append(boundaries, symbolBoundary{f.start, len(lines) - 1, f.symbol, f.kind})
	}

	if len(boundaries) == 0 {
		return nil
	}

	return chunkBySymbol(filePath, lines, "go", boundaries)
}
