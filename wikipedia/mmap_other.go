//go:build !unix

package wikipedia

import "os"

func mmapFile(f *os.File, size int) (data []byte, mmapped bool, err error) {
	b, err := readAll(f, size)
	return b, false, err
}

func munmap(b []byte) error { return nil }
