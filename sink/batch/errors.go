package batch

import "errors"

// ErrFlusherClosed is returned by Add when the Flusher has been closed.
var ErrFlusherClosed = errors.New("batch: flusher closed")
