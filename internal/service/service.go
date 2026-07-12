package service

import "context"

// Runner is the long-running work executed while the Windows service is Running.
// It should return when ctx is cancelled (service stop/shutdown).
type Runner func(ctx context.Context) error
