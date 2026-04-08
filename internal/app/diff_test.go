package app_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/erfianugrah/composer/internal/app"
)

func TestComputeDiff_NoChanges(t *testing.T) {
	content := "services:\n  web:\n    image: nginx\n"
	result := app.ComputeDiff(content, content)
	assert.False(t, result.HasChanges)
	assert.Equal(t, "No changes", result.Summary)
}

func TestComputeDiff_Addition(t *testing.T) {
	old := "services:\n  web:\n    image: nginx\n"
	new := "services:\n  web:\n    image: nginx\n    ports:\n      - \"8080:80\"\n"

	result := app.ComputeDiff(old, new)
	assert.True(t, result.HasChanges)

	added := 0
	for _, line := range result.Lines {
		if line.Type == "added" {
			added++
		}
	}
	assert.Greater(t, added, 0)
}

func TestComputeDiff_Removal(t *testing.T) {
	old := "services:\n  web:\n    image: nginx\n    ports:\n      - \"8080:80\"\n"
	new := "services:\n  web:\n    image: nginx\n"

	result := app.ComputeDiff(old, new)
	assert.True(t, result.HasChanges)

	removed := 0
	for _, line := range result.Lines {
		if line.Type == "removed" {
			removed++
		}
	}
	assert.Greater(t, removed, 0)
}

func TestComputeDiff_Modification(t *testing.T) {
	old := "services:\n  web:\n    image: nginx:alpine\n"
	new := "services:\n  web:\n    image: nginx:latest\n"

	result := app.ComputeDiff(old, new)
	assert.True(t, result.HasChanges)

	// Should have both a removal (old image) and addition (new image)
	hasRemoved, hasAdded := false, false
	for _, line := range result.Lines {
		if line.Type == "removed" {
			hasRemoved = true
		}
		if line.Type == "added" {
			hasAdded = true
		}
	}
	assert.True(t, hasRemoved)
	assert.True(t, hasAdded)
}

func TestComputeDiff_EmptyToContent(t *testing.T) {
	result := app.ComputeDiff("", "line1\nline2\n")
	assert.True(t, result.HasChanges)
}

func TestComputeDiff_ContentToEmpty(t *testing.T) {
	result := app.ComputeDiff("line1\nline2\n", "")
	assert.True(t, result.HasChanges)
}
