package app

import (
	"fmt"
	"strings"
)

// DiffResult holds the result of comparing two compose files.
type DiffResult struct {
	HasChanges bool       `json:"has_changes"`
	Lines      []DiffLine `json:"lines"`
	Summary    string     `json:"summary"`
}

// DiffLine represents a single line in a unified diff.
type DiffLine struct {
	Type    string `json:"type"` // "context", "added", "removed"
	Content string `json:"content"`
	OldLine int    `json:"old_line,omitempty"`
	NewLine int    `json:"new_line,omitempty"`
}

// ComputeDiff generates a simple unified diff between two strings.
func ComputeDiff(oldContent, newContent string) DiffResult {
	oldLines := strings.Split(oldContent, "\n")
	newLines := strings.Split(newContent, "\n")

	if oldContent == newContent {
		return DiffResult{HasChanges: false, Summary: "No changes"}
	}

	// Simple line-by-line diff using longest common subsequence
	result := DiffResult{HasChanges: true}

	lcs := lcsLines(oldLines, newLines)
	added, removed := 0, 0

	oldIdx, newIdx, lcsIdx := 0, 0, 0
	for oldIdx < len(oldLines) || newIdx < len(newLines) {
		if lcsIdx < len(lcs) && oldIdx < len(oldLines) && oldLines[oldIdx] == lcs[lcsIdx] &&
			newIdx < len(newLines) && newLines[newIdx] == lcs[lcsIdx] {
			// Context line (unchanged)
			result.Lines = append(result.Lines, DiffLine{
				Type: "context", Content: oldLines[oldIdx],
				OldLine: oldIdx + 1, NewLine: newIdx + 1,
			})
			oldIdx++
			newIdx++
			lcsIdx++
		} else if oldIdx < len(oldLines) && (lcsIdx >= len(lcs) || oldLines[oldIdx] != lcs[lcsIdx]) {
			// Removed line
			result.Lines = append(result.Lines, DiffLine{
				Type: "removed", Content: oldLines[oldIdx],
				OldLine: oldIdx + 1,
			})
			oldIdx++
			removed++
		} else if newIdx < len(newLines) && (lcsIdx >= len(lcs) || newLines[newIdx] != lcs[lcsIdx]) {
			// Added line
			result.Lines = append(result.Lines, DiffLine{
				Type: "added", Content: newLines[newIdx],
				NewLine: newIdx + 1,
			})
			newIdx++
			added++
		}
	}

	result.Summary = fmt.Sprintf("%d additions, %d deletions", added, removed)
	return result
}

// lcsLines computes the longest common subsequence of two line slices.
func lcsLines(a, b []string) []string {
	m, n := len(a), len(b)
	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}

	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			if a[i-1] == b[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else if dp[i-1][j] > dp[i][j-1] {
				dp[i][j] = dp[i-1][j]
			} else {
				dp[i][j] = dp[i][j-1]
			}
		}
	}

	// Backtrack to find the LCS
	result := make([]string, 0, dp[m][n])
	i, j := m, n
	for i > 0 && j > 0 {
		if a[i-1] == b[j-1] {
			result = append([]string{a[i-1]}, result...)
			i--
			j--
		} else if dp[i-1][j] > dp[i][j-1] {
			i--
		} else {
			j--
		}
	}

	return result
}
