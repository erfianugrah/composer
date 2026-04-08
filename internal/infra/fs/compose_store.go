package fs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ComposeStore handles reading and writing compose files on disk.
type ComposeStore struct {
	stacksDir string
}

func NewComposeStore(stacksDir string) *ComposeStore {
	return &ComposeStore{stacksDir: stacksDir}
}

// safeName validates that a stack name cannot escape the stacks directory.
func safeName(name string) error {
	if name == "" {
		return fmt.Errorf("stack name is empty")
	}
	if strings.Contains(name, "..") || strings.Contains(name, "/") || strings.Contains(name, "\\") || strings.Contains(name, "\x00") {
		return fmt.Errorf("invalid stack name: %q", name)
	}
	return nil
}

// Read returns the compose.yaml content for a stack.
func (s *ComposeStore) Read(stackName string) (string, error) {
	if err := safeName(stackName); err != nil {
		return "", err
	}
	path := filepath.Join(s.stacksDir, stackName, "compose.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading compose file: %w", err)
	}
	return string(data), nil
}

// Write writes compose.yaml content for a stack, creating the directory if needed.
func (s *ComposeStore) Write(stackName, content string) error {
	if err := safeName(stackName); err != nil {
		return err
	}
	dir := filepath.Join(s.stacksDir, stackName)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating stack directory: %w", err)
	}
	path := filepath.Join(dir, "compose.yaml")
	return os.WriteFile(path, []byte(content), 0644)
}

// Exists returns true if the compose.yaml exists for a stack.
func (s *ComposeStore) Exists(stackName string) bool {
	if safeName(stackName) != nil {
		return false
	}
	path := filepath.Join(s.stacksDir, stackName, "compose.yaml")
	_, err := os.Stat(path)
	return err == nil
}

// Delete removes the entire stack directory.
func (s *ComposeStore) Delete(stackName string) error {
	if err := safeName(stackName); err != nil {
		return err
	}
	dir := filepath.Join(s.stacksDir, stackName)
	// Final safety: resolved path must be inside stacksDir
	absDir, _ := filepath.Abs(dir)
	absBase, _ := filepath.Abs(s.stacksDir)
	if !strings.HasPrefix(absDir, absBase+string(filepath.Separator)) {
		return fmt.Errorf("resolved path %s escapes stacks directory", absDir)
	}
	return os.RemoveAll(dir)
}

// StackDir returns the full path to a stack's directory.
func (s *ComposeStore) StackDir(stackName string) string {
	return filepath.Join(s.stacksDir, stackName)
}
