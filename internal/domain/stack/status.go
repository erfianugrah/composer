package stack

// Status represents the runtime state of a stack.
type Status string

const (
	StatusRunning Status = "running"
	StatusStopped Status = "stopped"
	StatusPartial Status = "partial"
	StatusError   Status = "error"
	StatusSyncing Status = "syncing"
	StatusUnknown Status = "unknown"
)

func (s Status) Valid() bool {
	switch s {
	case StatusRunning, StatusStopped, StatusPartial, StatusError, StatusSyncing, StatusUnknown:
		return true
	}
	return false
}

// Source indicates how a stack's compose files are managed.
type Source string

const (
	SourceLocal Source = "local"
	SourceGit   Source = "git"
)

func (s Source) Valid() bool {
	return s == SourceLocal || s == SourceGit
}

// GitSyncStatus tracks the state of a git-backed stack relative to its remote.
type GitSyncStatus string

const (
	GitSynced   GitSyncStatus = "synced"
	GitBehind   GitSyncStatus = "behind"
	GitDiverged GitSyncStatus = "diverged"
	GitSyncErr  GitSyncStatus = "error"
	GitSyncing  GitSyncStatus = "syncing"
)
