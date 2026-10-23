package dto

// TaskFilesResponse lists the files of a task's workspace. Source is "workspace" for the live worktree or
// "committed" for the task branch after its worktree was removed.
type TaskFilesResponse struct {
	TaskID string   `json:"task_id"`
	Source string   `json:"source"`
	Files  []string `json:"files"`
}

// TaskFileResponse is one file of a task's workspace. Content is empty for binary files.
type TaskFileResponse struct {
	TaskID    string `json:"task_id"`
	Path      string `json:"path"`
	Source    string `json:"source"`
	Size      int64  `json:"size"`
	Content   string `json:"content"`
	Binary    bool   `json:"binary"`
	Truncated bool   `json:"truncated"`
}
