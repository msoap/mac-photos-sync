package filesystem

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

type Identity struct {
	Device, Inode uint64
	Size          int64
}

func Stat(path string) (Identity, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return Identity{}, err
	}
	if !fi.Mode().IsRegular() {
		return Identity{}, fmt.Errorf("not a regular file: %s", path)
	}
	statInfo, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return Identity{}, errors.New("filesystem identity unavailable")
	}
	return Identity{uint64(statInfo.Dev), uint64(statInfo.Ino), fi.Size()}, nil
}

func Same(a, b Identity) bool { return a.Device == b.Device && a.Inode == b.Inode }

func Device(path string) (uint64, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	statInfo, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, errors.New("filesystem device unavailable")
	}
	return uint64(statInfo.Dev), nil
}

func Link(source, dest string) error {
	if err := os.Link(source, dest); err != nil {
		if errors.Is(err, syscall.EXDEV) {
			return errors.New("cannot create hard links: source and destination are on different filesystems")
		}
		return err
	}
	sourceID, err := Stat(source)
	if err != nil {
		return err
	}
	destID, err := Stat(dest)
	if err != nil {
		return err
	}
	if !Same(sourceID, destID) {
		return fmt.Errorf("hard link verification failed: %s", dest)
	}
	return nil
}
