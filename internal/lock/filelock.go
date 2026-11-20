package lock

import (
	"errors"
	"os"
)

var ErrLocked = errors.New("another ArchieLink process holds the global network lock")

type Handle struct {
	file *os.File
}

func (h *Handle) Close() error {
	if h == nil || h.file == nil {
		return nil
	}
	err := h.file.Close()
	h.file = nil
	return err
}
