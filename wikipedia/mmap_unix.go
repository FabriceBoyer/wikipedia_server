//go:build unix

package wikipedia

import (
	"os"
	"syscall"
)

func mmapFile(f *os.File, size int) (data []byte, mmapped bool, err error) {
	if size <= 0 {
		return nil, false, syscall.EINVAL
	}
	b, err := syscall.Mmap(int(f.Fd()), 0, size, syscall.PROT_READ, syscall.MAP_SHARED)
	if err != nil {
		// Fall back to a plain read; mmap can fail on exotic filesystems.
		data, rerr := readAll(f, size)
		return data, false, rerr
	}
	return b, true, nil
}

func munmap(b []byte) error {
	return syscall.Munmap(b)
}
