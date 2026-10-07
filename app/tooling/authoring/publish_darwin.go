//go:build darwin

package authoring

import "golang.org/x/sys/unix"

func renameExclusive(from, to string) error { return unix.RenamexNp(from, to, unix.RENAME_EXCL) }
