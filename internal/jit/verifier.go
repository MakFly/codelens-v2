package jit

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/cespare/xxhash/v2"
)

// Citation points to specific lines in a file, with a hash for JIT verification.
type Citation struct {
	FilePath  string `json:"file"`
	LineStart int    `json:"line_start"`
	LineEnd   int    `json:"line_end"`
	Hash      string `json:"hash"` // xxhash64 of the cited lines at creation time
}

// Verifier checks if citations still match current code.
type Verifier struct {
	projectRoot string
}

func New(projectRoot string) *Verifier {
	return &Verifier{projectRoot: projectRoot}
}

// VerifyCitation returns true if the cited lines still match the stored hash.
func (v *Verifier) VerifyCitation(c Citation) bool {
	path := v.resolvePath(c.FilePath)
	lines, err := readLines(path, c.LineStart, c.LineEnd)
	if err != nil {
		return false
	}

	currentHash := hashLines(lines)
	return currentHash == c.Hash
}

// HashCitation computes the hash for a citation at the current moment.
// Call this when creating a new memory to store the "snapshot" hash.
func (v *Verifier) HashCitation(c *Citation) error {
	path := v.resolvePath(c.FilePath)
	lines, err := readLines(path, c.LineStart, c.LineEnd)
	if err != nil {
		return fmt.Errorf("read lines for citation %s:%d-%d: %w", c.FilePath, c.LineStart, c.LineEnd, err)
	}
	c.Hash = hashLines(lines)
	return nil
}

// VerifyAll checks all citations in a slice. Returns true only if ALL are valid.
func (v *Verifier) VerifyAll(citations []Citation) bool {
	for _, c := range citations {
		if !v.VerifyCitation(c) {
			return false
		}
	}
	return true
}

func (v *Verifier) resolvePath(filePath string) string {
	if len(filePath) > 0 && filePath[0] == '/' {
		return filePath
	}
	return v.projectRoot + "/" + filePath
}

func readLines(filePath string, start, end int) ([]string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	lineNum := 1

	for scanner.Scan() {
		if lineNum >= start && lineNum <= end {
			lines = append(lines, scanner.Text())
		}
		if lineNum > end {
			break
		}
		lineNum++
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	if len(lines) == 0 {
		return nil, fmt.Errorf("lines %d-%d not found in %s", start, end, filePath)
	}

	return lines, nil
}

func hashLines(lines []string) string {
	h := xxhash.New()
	for _, l := range lines {
		h.Write([]byte(l))
		h.Write([]byte("\n"))
	}
	return strconv.FormatUint(h.Sum64(), 16)
}

// HashContent computes xxhash of arbitrary content (used for file-level change detection).
func HashContent(content string) string {
	return strconv.FormatUint(xxhash.Sum64String(content), 16)
}

// ReadFileLines is exported for use in read_file_smart tool.
func ReadFileLines(filePath string, start, end int) ([]string, error) {
	return readLines(filePath, start, end)
}

// LineCount returns the number of lines in a file.
func LineCount(filePath string) (int, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	count := 0
	scanner := bufio.NewScanner(f)
	// Use larger buffer for wide lines
	scanner.Buffer(make([]byte, 64*1024), 64*1024)
	for scanner.Scan() {
		count++
	}
	return count, scanner.Err()
}

// FormatCitationRef returns a human-readable citation reference.
func FormatCitationRef(c Citation) string {
	return fmt.Sprintf("%s:%d-%d", c.FilePath, c.LineStart, c.LineEnd)
}

// ParseCitationRef parses "file.go:10-25" into a Citation (without hash).
func ParseCitationRef(ref string) (Citation, error) {
	// Format: "path/file.go:start-end"
	lastColon := strings.LastIndex(ref, ":")
	if lastColon < 0 {
		return Citation{}, fmt.Errorf("invalid citation ref: %s", ref)
	}
	filePath := ref[:lastColon]
	lineRange := ref[lastColon+1:]

	parts := strings.Split(lineRange, "-")
	if len(parts) != 2 {
		return Citation{}, fmt.Errorf("invalid line range: %s", lineRange)
	}

	start, err := strconv.Atoi(parts[0])
	if err != nil {
		return Citation{}, err
	}
	end, err := strconv.Atoi(parts[1])
	if err != nil {
		return Citation{}, err
	}

	return Citation{FilePath: filePath, LineStart: start, LineEnd: end}, nil
}
