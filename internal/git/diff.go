package git

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// FileStat holds diff statistics for an individual file.
type FileStat struct {
	Path      string `json:"path"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Status    string `json:"status"` // "added", "modified", "deleted"
}

// Result holds the raw diff and aggregate statistics of a snapshot.
type Result struct {
	RawDiff        string     `json:"raw_diff"`
	Files          []FileStat `json:"files"`
	TotalAdditions int        `json:"total_additions"`
	TotalDeletions int        `json:"total_deletions"`
	HasChanges     bool       `json:"has_changes"`
}

// ParseDiffSummary parses a raw unified diff and extracts FileStat for each file.
func ParseDiffSummary(rawDiff string) ([]FileStat, error) {
	if strings.TrimSpace(rawDiff) == "" {
		return []FileStat{}, nil
	}

	var files []FileStat
	var current *FileStat
	scanner := bufio.NewScanner(strings.NewReader(rawDiff))
	inHunk := false

	flushCurrent := func() {
		if current != nil {
			files = append(files, *current)
			current = nil
			inHunk = false
		}
	}

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "diff --git ") {
			flushCurrent()

			// diff --git a/path b/path
			parts := strings.Split(line, " ")
			path := ""
			if len(parts) >= 4 {
				path = strings.TrimPrefix(parts[3], "b/")
			}
			current = &FileStat{
				Path:   path,
				Status: "modified", // default unless new/deleted file mode is encountered
			}
			inHunk = false
			continue
		}

		if current == nil {
			continue
		}

		if strings.HasPrefix(line, "new file mode ") {
			current.Status = "added"
			continue
		}
		if strings.HasPrefix(line, "deleted file mode ") {
			current.Status = "deleted"
			continue
		}
		if strings.HasPrefix(line, "rename to ") {
			current.Path = strings.TrimPrefix(line, "rename to ")
			current.Status = "modified"
			continue
		}
		if strings.HasPrefix(line, "+++ b/") {
			current.Path = strings.TrimPrefix(line, "+++ b/")
			continue
		}
		if strings.HasPrefix(line, "--- a/") && current.Status == "deleted" {
			current.Path = strings.TrimPrefix(line, "--- a/")
			continue
		}

		if strings.HasPrefix(line, "@@") {
			inHunk = true
			continue
		}

		if inHunk {
			if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
				current.Additions++
			} else if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
				current.Deletions++
			}
		}
	}

	flushCurrent()

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed scanning raw diff: %w", err)
	}

	return files, nil
}

// CaptureSnapshot captures staged, unstaged, and untracked changes in the given worktree,
// creating a local commit if needed, and computes a diff against baseBranch.
func CaptureSnapshot(ctx context.Context, worktreePath, baseBranch string) (*Result, error) {
	// 1. Run git status --porcelain to check for uncommitted/untracked changes
	cmdStatus := exec.CommandContext(ctx, "git", "status", "--porcelain")
	cmdStatus.Dir = worktreePath
	statusOut, err := cmdStatus.Output()
	if err != nil {
		return nil, fmt.Errorf("git status failed in %q: %w", worktreePath, err)
	}

	if len(bytes.TrimSpace(statusOut)) > 0 {
		// Stage untracked/unstaged changes
		cmdAdd := exec.CommandContext(ctx, "git", "add", "-A")
		cmdAdd.Dir = worktreePath
		var addStderr bytes.Buffer
		cmdAdd.Stderr = &addStderr
		if err := cmdAdd.Run(); err != nil {
			return nil, fmt.Errorf("git add -A failed in %q: %w (stderr: %s)", worktreePath, err, addStderr.String())
		}

		// Ensure git user identity is configured locally if not already set
		_ = exec.CommandContext(ctx, "git", "-C", worktreePath, "config", "user.name", "Agent Task Manager").Run()
		_ = exec.CommandContext(ctx, "git", "-C", worktreePath, "config", "user.email", "agent@taskmanager.local").Run()

		// Commit staged changes
		cmdCommit := exec.CommandContext(ctx, "git", "commit", "-m", "chore(review): capture agent changes for review")
		cmdCommit.Dir = worktreePath
		var commitStderr bytes.Buffer
		cmdCommit.Stderr = &commitStderr
		if err := cmdCommit.Run(); err != nil {
			// In case nothing was staged or hook prevented, check if status changed
			return nil, fmt.Errorf("git commit failed in %q: %w (stderr: %s)", worktreePath, err, commitStderr.String())
		}
	}

	// 2. Compute diff against baseBranch
	// If baseBranch is empty, diff against empty tree: 4b825dc642cb6eb9a060e54bf8d69288fbee4904
	var diffArgs []string
	if baseBranch == "" {
		diffArgs = []string{"diff", "4b825dc642cb6eb9a060e54bf8d69288fbee4904", "HEAD"}
	} else {
		// Check if baseBranch exists: git rev-parse --verify <baseBranch>
		revCheck := exec.CommandContext(ctx, "git", "rev-parse", "--verify", baseBranch)
		revCheck.Dir = worktreePath
		if err := revCheck.Run(); err != nil {
			// baseBranch does not exist, diff against empty tree
			diffArgs = []string{"diff", "4b825dc642cb6eb9a060e54bf8d69288fbee4904", "HEAD"}
		} else {
			diffArgs = []string{"diff", fmt.Sprintf("%s...HEAD", baseBranch)}
		}
	}

	cmdDiff := exec.CommandContext(ctx, "git", diffArgs...)
	cmdDiff.Dir = worktreePath
	var diffStderr bytes.Buffer
	cmdDiff.Stderr = &diffStderr
	rawDiffBytes, err := cmdDiff.Output()
	if err != nil {
		return nil, fmt.Errorf("git diff failed in %q: %w (stderr: %s)", worktreePath, err, diffStderr.String())
	}

	rawDiff := string(rawDiffBytes)
	fileStats, err := ParseDiffSummary(rawDiff)
	if err != nil {
		return nil, fmt.Errorf("failed to parse diff summary: %w", err)
	}

	totalAdditions := 0
	totalDeletions := 0
	for _, f := range fileStats {
		totalAdditions += f.Additions
		totalDeletions += f.Deletions
	}

	return &Result{
		RawDiff:        rawDiff,
		Files:          fileStats,
		TotalAdditions: totalAdditions,
		TotalDeletions: totalDeletions,
		HasChanges:     len(fileStats) > 0,
	}, nil
}
