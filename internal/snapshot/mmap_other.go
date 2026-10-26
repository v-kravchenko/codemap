//go:build !unix

package snapshot

import "os"

func readMapped(path string) ([]byte, error) { return os.ReadFile(path) }
