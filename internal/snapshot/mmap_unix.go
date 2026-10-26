//go:build unix

package snapshot

import (
	"os"
	"syscall"
)

// readMapped: the file mapped read-only (never unmapped: the process ends after the query); only
// the pages a query touches are read.
func readMapped(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.Size() == 0 {
		return os.ReadFile(path)
	}
	return syscall.Mmap(int(f.Fd()), 0, int(st.Size()), syscall.PROT_READ, syscall.MAP_SHARED)
}
