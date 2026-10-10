package resultengine

import (
	"bufio"
	"container/heap"
	"context"
	"encoding/json"
	"io"
	"os"
	"sort"
)

// Sorting bounds both run size and merge fan-in. Rows are spilled as soon as
// their encoded size reaches 4 MiB; merging opens at most 32 runs at a time.
type sortEntry struct {
	Ordinal int64                  `json:"ordinal"`
	Key     sortKey                `json:"key"`
	Row     map[string]interface{} `json:"row,omitempty"`
	CSV     string                 `json:"csv,omitempty"`
}
type topRows struct {
	s     *sorter
	limit int
	rows  []sortEntry
}

func (t topRows) Len() int            { return len(t.rows) }
func (t topRows) Less(i, j int) bool  { return t.s.less(t.rows[j], t.rows[i]) }
func (t topRows) Swap(i, j int)       { t.rows[i], t.rows[j] = t.rows[j], t.rows[i] }
func (t *topRows) Push(v interface{}) { t.rows = append(t.rows, v.(sortEntry)) }
func (t *topRows) Pop() interface{} {
	v := t.rows[len(t.rows)-1]
	t.rows = t.rows[:len(t.rows)-1]
	return v
}
func (t *topRows) add(v sortEntry) {
	if len(t.rows) < t.limit {
		heap.Push(t, v)
	} else if t.s.less(v, t.rows[0]) {
		t.rows[0] = v
		heap.Fix(t, 0)
	}
}
func (t *topRows) each(emit func(sortEntry) error) error {
	sort.Slice(t.rows, func(i, j int) bool { return t.s.less(t.rows[i], t.rows[j]) })
	for _, v := range t.rows {
		if err := t.s.ctx.Err(); err != nil {
			return err
		}
		if err := emit(v); err != nil {
			return err
		}
	}
	return nil
}

type sorter struct {
	ctx    context.Context
	dir    string
	desc   bool
	buffer []sortEntry
	bytes  int
	runs   []string
}

func newSorter(ctx context.Context, root string, desc bool) (*sorter, error) {
	dir, err := os.MkdirTemp(root, "octopus-sort-*")
	if err != nil {
		return nil, err
	}
	return &sorter{ctx: ctx, dir: dir, desc: desc}, nil
}
func (s *sorter) close() { os.RemoveAll(s.dir) }
func (s *sorter) less(a, b sortEntry) bool {
	if a.Key.Missing != b.Key.Missing {
		return !a.Key.Missing
	}
	cmp := 0
	if a.Key.Number != nil && b.Key.Number != nil {
		if *a.Key.Number < *b.Key.Number {
			cmp = -1
		}
		if *a.Key.Number > *b.Key.Number {
			cmp = 1
		}
	} else if !a.Key.Missing {
		if a.Key.Text < b.Key.Text {
			cmp = -1
		}
		if a.Key.Text > b.Key.Text {
			cmp = 1
		}
	}
	if cmp == 0 {
		return a.Ordinal < b.Ordinal
	}
	if s.desc {
		return cmp > 0
	}
	return cmp < 0
}
func (s *sorter) add(v sortEntry) error {
	if v.Row != nil {
		b, err := json.Marshal(v.Row)
		if err != nil {
			return err
		}
		s.bytes += len(b)
	}
	s.bytes += len(v.CSV) + len(v.Key.Text) + 30
	s.buffer = append(s.buffer, v)
	if s.bytes >= 4<<20 || len(s.buffer) >= 4000 {
		return s.flush()
	}
	return nil
}
func (s *sorter) flush() error {
	if len(s.buffer) == 0 {
		return nil
	}
	if err := s.ctx.Err(); err != nil {
		return err
	}
	sort.SliceStable(s.buffer, func(i, j int) bool { return s.less(s.buffer[i], s.buffer[j]) })
	file, err := os.CreateTemp(s.dir, "run-*.bin")
	if err != nil {
		return err
	}
	w := bufio.NewWriterSize(file, 65536)
	for _, v := range s.buffer {
		if err = writeEntry(w, v); err != nil {
			file.Close()
			return err
		}
	}
	err = w.Flush()
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	s.runs = append(s.runs, file.Name())
	s.buffer = nil
	s.bytes = 0
	return nil
}

type runCursor struct {
	file   *os.File
	reader *bufio.Reader
	entry  sortEntry
}
type runHeap struct {
	cursors []*runCursor
	s       *sorter
}

func (h runHeap) Len() int            { return len(h.cursors) }
func (h runHeap) Less(i, j int) bool  { return h.s.less(h.cursors[i].entry, h.cursors[j].entry) }
func (h runHeap) Swap(i, j int)       { h.cursors[i], h.cursors[j] = h.cursors[j], h.cursors[i] }
func (h *runHeap) Push(v interface{}) { h.cursors = append(h.cursors, v.(*runCursor)) }
func (h *runHeap) Pop() interface{} {
	v := h.cursors[len(h.cursors)-1]
	h.cursors = h.cursors[:len(h.cursors)-1]
	return v
}
func (s *sorter) merge(paths []string, emit func(sortEntry) error) error {
	h := &runHeap{s: s}
	opened := []*os.File{}
	defer func() {
		for _, f := range opened {
			f.Close()
		}
	}()
	for _, path := range paths {
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		opened = append(opened, f)
		c := &runCursor{file: f, reader: bufio.NewReaderSize(f, 65536)}
		if err = readEntry(c.reader, &c.entry); err == io.EOF {
			continue
		} else if err != nil {
			return err
		}
		heap.Push(h, c)
	}
	for h.Len() > 0 {
		if err := s.ctx.Err(); err != nil {
			return err
		}
		c := heap.Pop(h).(*runCursor)
		if err := emit(c.entry); err != nil {
			return err
		}
		c.entry = sortEntry{}
		if err := readEntry(c.reader, &c.entry); err == nil {
			heap.Push(h, c)
		} else if err != io.EOF {
			return err
		}
	}
	return nil
}
func (s *sorter) each(emit func(sortEntry) error) error {
	if len(s.runs) == 0 {
		sort.SliceStable(s.buffer, func(i, j int) bool { return s.less(s.buffer[i], s.buffer[j]) })
		for _, v := range s.buffer {
			if err := s.ctx.Err(); err != nil {
				return err
			}
			if err := emit(v); err != nil {
				return err
			}
		}
		return nil
	}
	if err := s.flush(); err != nil {
		return err
	}
	for len(s.runs) > 32 {
		next := []string{}
		for start := 0; start < len(s.runs); start += 32 {
			end := start + 32
			if end > len(s.runs) {
				end = len(s.runs)
			}
			f, err := os.CreateTemp(s.dir, "merge-*.bin")
			if err != nil {
				return err
			}
			w := bufio.NewWriterSize(f, 65536)
			err = s.merge(s.runs[start:end], func(v sortEntry) error { return writeEntry(w, v) })
			if err == nil {
				err = w.Flush()
			}
			closeErr := f.Close()
			if err == nil {
				err = closeErr
			}
			if err != nil {
				return err
			}
			next = append(next, f.Name())
			for _, path := range s.runs[start:end] {
				if err = os.Remove(path); err != nil {
					return err
				}
			}
		}
		s.runs = next
	}
	return s.merge(s.runs, emit)
}
