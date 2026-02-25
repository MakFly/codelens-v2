package indexer

import (
	"regexp"
	"strings"
)

var (
	rustFnStart     = regexp.MustCompile(`^\s*(?:pub(?:\(crate\))?\s+)?(?:async\s+)?fn\s+(\w+)`)
	rustImplStart   = regexp.MustCompile(`^\s*(?:pub\s+)?impl(?:<[^>]+>)?\s+(?:(\w+)\s+for\s+)?(\w+)`)
	rustStructStart = regexp.MustCompile(`^\s*(?:pub(?:\(crate\))?\s+)?(?:struct|enum)\s+(\w+)`)
	rustTraitStart  = regexp.MustCompile(`^\s*(?:pub(?:\(crate\))?\s+)?trait\s+(\w+)`)
)

func chunkRust(filePath string, lines []string) []Chunk {
	var boundaries []symbolBoundary
	depth := 0

	type frame struct {
		start  int
		symbol string
		kind   string
		depth  int
	}
	var stack []frame
	var currentImpl string

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

		// impl block at depth 0
		if m := rustImplStart.FindStringSubmatch(line); m != nil && prevDepth == 0 {
			currentImpl = m[2] // the type name (last capture)
			stack = append(stack, frame{i, currentImpl, "impl", prevDepth + 1})
			continue
		}

		// fn detection
		if m := rustFnStart.FindStringSubmatch(line); m != nil {
			if prevDepth == 1 && currentImpl != "" {
				// method inside impl block
				stack = append(stack, frame{i, currentImpl + "." + m[1], "method", prevDepth + 1})
			} else if prevDepth == 0 {
				stack = append(stack, frame{i, m[1], "function", prevDepth + 1})
			}
			continue
		}

		// struct/enum at depth 0
		if m := rustStructStart.FindStringSubmatch(line); m != nil && prevDepth == 0 {
			kind := "struct"
			if strings.Contains(trimmed, "enum") {
				kind = "enum"
			}
			stack = append(stack, frame{i, m[1], kind, prevDepth + 1})
			continue
		}

		// trait at depth 0
		if m := rustTraitStart.FindStringSubmatch(line); m != nil && prevDepth == 0 {
			stack = append(stack, frame{i, m[1], "trait", prevDepth + 1})
			continue
		}

		// pop stack when depth drops below frame depth
		for len(stack) > 0 && depth < stack[len(stack)-1].depth {
			top := stack[len(stack)-1]
			boundaries = append(boundaries, symbolBoundary{top.start, i, top.symbol, top.kind})
			stack = stack[:len(stack)-1]
			// clear currentImpl when its frame is popped
			if top.kind == "impl" {
				currentImpl = ""
			}
		}
	}

	// flush remaining frames
	for _, f := range stack {
		boundaries = append(boundaries, symbolBoundary{f.start, len(lines) - 1, f.symbol, f.kind})
	}

	if len(boundaries) == 0 {
		return nil
	}

	return chunkBySymbol(filePath, lines, "rust", boundaries)
}
