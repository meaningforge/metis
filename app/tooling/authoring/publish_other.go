//go:build !linux && !darwin

package authoring

import "fmt"

func renameExclusive(from, to string) error {
	return fmt.Errorf("atomic no-replace directory publication is unsupported on this platform")
}
