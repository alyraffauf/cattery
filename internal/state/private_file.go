package state

import (
	"os"
	"syscall"
)

func preparePrivateFile(path string) error {
	handle, err := os.OpenFile(path, os.O_RDWR|syscall.O_NOFOLLOW, stateFileMode)
	if err == nil {
		defer handle.Close()
		info, statErr := handle.Stat()
		if statErr != nil {
			return statErr
		}
		return verifyPrivateFile(path, info)
	}
	if !os.IsNotExist(err) {
		return err
	}
	return createPrivateFile(path)
}

func verifyPrivateFile(path string, info os.FileInfo) error {
	if !info.Mode().IsRegular() {
		return errNotRegular(path, info.Mode())
	}
	if info.Mode().Perm() != stateFileMode {
		return errWrongFileMode(path, info.Mode().Perm())
	}
	return nil
}

func createPrivateFile(path string) error {
	handle, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR|syscall.O_NOFOLLOW, stateFileMode)
	if err != nil {
		return err
	}
	info, err := handle.Stat()
	if err != nil {
		_ = handle.Close()
		return err
	}
	if err := verifyPrivateFile(path, info); err != nil {
		_ = handle.Close()
		return err
	}
	if err := handle.Chmod(stateFileMode); err != nil {
		_ = handle.Close()
		return err
	}
	if err := handle.Close(); err != nil {
		return err
	}
	return nil
}
