package wikipedia

import (
	"io"
	"os"
)

func readAll(f *os.File, size int) ([]byte, error) {
	b := make([]byte, size)
	if _, err := io.ReadFull(io.NewSectionReader(f, 0, int64(size)), b); err != nil {
		return nil, err
	}
	return b, nil
}
