package main

import (
	"bufio"
	"encoding/base64"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"unicode"
)

const (
	maxFileSize   = 100 * 1024 * 1024 // 100MB max file size
	maxMatches    = 10000              // Maximum number of matches to process
	minB64Length  = 4                  // Minimum base64 string length
	chunkSize     = 64 * 1024          // Read buffer size
)

type Config struct {
	urlSafe    bool
	minLength  int
	maxSize    int64
	verbose    bool
	showOffset bool
}

var (
	config Config
	// Standard base64 pattern
	stdB64Pattern = regexp.MustCompile(`[A-Za-z0-9+/]{4,}={0,2}`)
	// URL-safe base64 pattern
	urlB64Pattern = regexp.MustCompile(`[A-Za-z0-9\-_]{4,}={0,2}`)
)

func init() {
	flag.BoolVar(&config.urlSafe, "url", false, "Also decode URL-safe base64 (with -_ instead of +/)")
	flag.IntVar(&config.minLength, "min", minB64Length, "Minimum base64 string length to decode")
	flag.Int64Var(&config.maxSize, "max-size", maxFileSize, "Maximum file size to process (bytes)")
	flag.BoolVar(&config.verbose, "v", false, "Verbose output (show errors and statistics)")
	flag.BoolVar(&config.showOffset, "offset", false, "Show byte offset of found strings")
}

func main() {
	flag.Parse()

	filename := flag.Arg(0)
	if filename == "" {
		fmt.Fprintln(os.Stderr, "usage: b64d [flags] <filename>")
		fmt.Fprintln(os.Stderr, "\nFlags:")
		flag.PrintDefaults()
		os.Exit(1)
	}

	if err := processFile(filename); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func processFile(filename string) error {
	// Check file size first
	info, err := os.Stat(filename)
	if err != nil {
		return fmt.Errorf("cannot stat file: %w", err)
	}

	if info.Size() > config.maxSize {
		return fmt.Errorf("file too large (%d bytes, max %d)", info.Size(), config.maxSize)
	}

	f, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("cannot open file: %w", err)
	}
	defer f.Close()

	return findAndDecode(f)
}

func findAndDecode(r io.Reader) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, chunkSize), chunkSize)
	
	var lineNum int
	var totalFound, totalDecoded int
	patterns := []string{}

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		
		// Find potential base64 strings
		matches := findBase64Patterns(line)
		
		for _, match := range matches {
			if totalFound >= maxMatches {
				if config.verbose {
					fmt.Fprintf(os.Stderr, "Warning: reached maximum match limit (%d)\n", maxMatches)
				}
				return nil
			}
			totalFound++

			decoded, err := decodeBase64(match)
			if err != nil {
				if config.verbose {
					fmt.Fprintf(os.Stderr, "Line %d: decode error for '%s': %v\n", lineNum, truncate(match, 20), err)
				}
				continue
			}

			if !isValidOutput(decoded) {
				continue
			}

			totalDecoded++
			if config.showOffset {
				fmt.Printf("Line %d: %s\n", lineNum, decoded)
			} else {
				fmt.Println(decoded)
			}
			
			if config.verbose {
				patterns = append(patterns, match)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read error: %w", err)
	}

	if config.verbose {
		fmt.Fprintf(os.Stderr, "\nStatistics:\n")
		fmt.Fprintf(os.Stderr, "  Total patterns found: %d\n", totalFound)
		fmt.Fprintf(os.Stderr, "  Successfully decoded: %d\n", totalDecoded)
	}

	return nil
}

func findBase64Patterns(content string) []string {
	var matches []string
	seen := make(map[string]bool)

	// Find standard base64
	for _, match := range stdB64Pattern.FindAllString(content, -1) {
		if len(match) >= config.minLength && isValidBase64Length(match) {
			if !seen[match] {
				matches = append(matches, match)
				seen[match] = true
			}
		}
	}

	// Find URL-safe base64 if enabled
	if config.urlSafe {
		for _, match := range urlB64Pattern.FindAllString(content, -1) {
			if len(match) >= config.minLength && isValidBase64Length(match) {
				if !seen[match] {
					matches = append(matches, match)
					seen[match] = true
				}
			}
		}
	}

	return matches
}

func isValidBase64Length(s string) bool {
	// Remove padding
	s = strings.TrimRight(s, "=")
	// Valid base64 should be 4n or 4n+2 or 4n+3 in length (after removing padding)
	rem := len(s) % 4
	return rem == 0 || rem == 2 || rem == 3
}

func decodeBase64(s string) (string, error) {
	// Try standard base64 first
	decoded, err := base64.StdEncoding.DecodeString(s)
	if err == nil {
		return string(decoded), nil
	}

	// Try URL-safe if enabled and standard failed
	if config.urlSafe {
		decoded, err = base64.URLEncoding.DecodeString(s)
		if err == nil {
			return string(decoded), nil
		}

		// Try raw URL encoding (no padding)
		decoded, err = base64.RawURLEncoding.DecodeString(s)
		if err == nil {
			return string(decoded), nil
		}
	}

	// Try raw standard encoding (no padding)
	decoded, err = base64.RawStdEncoding.DecodeString(s)
	if err == nil {
		return string(decoded), nil
	}

	return "", fmt.Errorf("invalid base64")
}

func isValidOutput(s string) bool {
	if s == "" {
		return false
	}

	// Check if string contains mostly printable ASCII
	printableCount := 0
	for _, r := range s {
		// Allow printable ASCII and common whitespace
		if unicode.IsPrint(r) || r == '\n' || r == '\r' || r == '\t' {
			printableCount++
		} else if !unicode.IsSpace(r) {
			// Non-printable, non-whitespace character
			return false
		}
	}

	// Require at least 75% printable characters
	return float64(printableCount)/float64(len(s)) >= 0.75
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}