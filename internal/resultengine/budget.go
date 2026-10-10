package resultengine

import (
	"fmt"
	"io"
)

// Per-operation ceilings bound disk consumption as well as memory. The service
// holds its admission lease until an exported response has been closed.
const maxExportBytes int64 = 1 << 30
const maxSpillBytes int64 = 4 << 30

type diskBudget struct{ used, limit int64 }
type budgetWriter struct {
	writer io.Writer
	budget *diskBudget
}

func (w budgetWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > w.budget.limit-w.budget.used {
		return 0, fmt.Errorf("result temporary disk budget exceeded")
	}
	n, err := w.writer.Write(p)
	w.budget.used += int64(n)
	return n, err
}
