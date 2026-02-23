package indexer

import (
	"regexp"
	"strings"
)

// PHP chunker: regex-based for v0.1, upgrade to tree-sitter in v0.4.
// Detects: class, interface, trait, abstract class, function (standalone).

var (
	phpClassStart    = regexp.MustCompile(`^(?:abstract\s+)?(?:class|interface|trait|enum)\s+(\w+)`)
	phpMethodStart   = regexp.MustCompile(`^\s+(?:public|protected|private|static|abstract|final|\s)*function\s+(\w+)\s*\(`)
	phpFunctionStart = regexp.MustCompile(`^(?:function)\s+(\w+)\s*\(`)
	phpBlockOpen     = regexp.MustCompile(`\{`)
	phpBlockClose    = regexp.MustCompile(`^\s*\}`)
)

func chunkPHP(filePath string, lines []string) []Chunk {
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
	currentClassKind := ""

	for i, raw := range lines {
		line := strings.TrimRight(raw, "\r")

		// Track brace depth
		opens := strings.Count(line, "{") - strings.Count(line, "//{"[1:]) // ignore commented
		closes := strings.Count(line, "}")
		prevDepth := depth
		depth += opens - closes

		// Detect class/interface/trait start
		if m := phpClassStart.FindStringSubmatch(line); m != nil {
			name := m[1]
			currentClass = name
			currentClassKind = classKind(line)
			stack = append(stack, frame{
				start:  i,
				symbol: name,
				kind:   currentClassKind,
				depth:  prevDepth + 1,
			})
			continue
		}

		// Detect method (inside class)
		if currentClass != "" && currentClassKind != "interface" {
			if m := phpMethodStart.FindStringSubmatch(line); m != nil {
				methodName := currentClass + "::" + m[1]
				stack = append(stack, frame{
					start:  i,
					symbol: methodName,
					kind:   "method",
					depth:  prevDepth + 1,
				})
				continue
			}
		}

		// Detect standalone function
		if currentClass == "" {
			if m := phpFunctionStart.FindStringSubmatch(line); m != nil {
				stack = append(stack, frame{
					start:  i,
					symbol: m[1],
					kind:   "function",
					depth:  prevDepth + 1,
				})
				continue
			}
		}

		// Pop frame when brace depth drops back to frame's entry depth
		for len(stack) > 0 {
			top := stack[len(stack)-1]
			if depth < top.depth {
				boundaries = append(boundaries, symbolBoundary{
					start:  top.start,
					end:    i,
					symbol: top.symbol,
					kind:   top.kind,
				})
				stack = stack[:len(stack)-1]

				// If we closed a class, clear currentClass
				if top.kind != "method" && top.kind != "function" {
					currentClass = ""
					currentClassKind = ""
				}
			} else {
				break
			}
		}
	}

	// Close any unclosed frames
	for _, f := range stack {
		boundaries = append(boundaries, symbolBoundary{
			start:  f.start,
			end:    len(lines) - 1,
			symbol: f.symbol,
			kind:   f.kind,
		})
	}

	if len(boundaries) == 0 {
		return nil
	}

	return chunkBySymbol(filePath, lines, "php", boundaries)
}

func classKind(line string) string {
	line = strings.ToLower(line)
	switch {
	case strings.Contains(line, "interface"):
		return "interface"
	case strings.Contains(line, "trait"):
		return "trait"
	case strings.Contains(line, "enum"):
		return "enum"
	default:
		return "class"
	}
}
