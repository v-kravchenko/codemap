//go:build !unix

package main

import "os"

func readMapped(path string) ([]byte, error) { return os.ReadFile(path) }
