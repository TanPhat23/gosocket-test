package common

import "errors"

// ErrRuntimeQuarantined indicates that task runtime execution is intentionally
// disabled. It is shared by the runner, service, queue, and API layers so each
// can detect it with errors.Is without importing one another.
// The message is user-visible and must stay generic.
var ErrRuntimeQuarantined = errors.New("runtime execution is temporarily disabled")
