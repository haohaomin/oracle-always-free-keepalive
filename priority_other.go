//go:build !linux

package main

// setPriority is a no-op on non-Linux platforms. Process niceness control is a
// Linux-specific concern for keepwarm (the OCI target); on darwin/dev it does
// nothing rather than failing.
func setPriority(nice int) error {
	return nil
}
