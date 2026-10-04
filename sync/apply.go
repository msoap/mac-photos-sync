package sync

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/msoap/mac-photos-sync/filesystem"
)

func CheckDevice(root string, plan Plan) error {
	parent := root
	for {
		if _, err := os.Stat(parent); err == nil {
			break
		}
		next := filepath.Dir(parent)
		if next == parent {
			return fmt.Errorf("destination parent does not exist: %s", root)
		}
		parent = next
	}
	dev, err := filesystem.Device(parent)
	if err != nil {
		return err
	}
	for _, op := range plan.Operations {
		if op.Source == "" {
			continue
		}
		src, err := filesystem.Stat(op.Source)
		if err != nil {
			return err
		}
		if src.Device != dev {
			return errors.New("cannot create hard links: source and destination are on different filesystems")
		}
	}
	return nil
}

func Apply(root string, plan Plan, log func(Operation)) error {
	for _, op := range plan.Operations {
		if op.Kind == "REMOVE_LINK" {
			continue
		}
		to := filepath.Join(root, op.To)
		if err := os.MkdirAll(filepath.Dir(to), 0755); err != nil {
			return err
		}
		if op.Kind == "REPAIR_LINK" {
			id, err := filesystem.Stat(to)
			if err != nil {
				return err
			}
			if !filesystem.Same(id, op.Expected) {
				return fmt.Errorf("destination changed during repair: %s", op.To)
			}
			var nonce [16]byte
			if _, err = rand.Read(nonce[:]); err != nil {
				return err
			}
			tmpName := filepath.Join(filepath.Dir(to), ".mac-photos-sync-"+hex.EncodeToString(nonce[:]))
			if err = filesystem.Link(op.Source, tmpName); err != nil {
				return err
			}
			id, err = filesystem.Stat(to)
			if err != nil || !filesystem.Same(id, op.Expected) {
				os.Remove(tmpName)
				return fmt.Errorf("destination changed during repair: %s", op.To)
			}
			if err = os.Rename(tmpName, to); err != nil {
				os.Remove(tmpName)
				return err
			}
			src, err := filesystem.Stat(op.Source)
			if err != nil {
				return err
			}
			dst, err := filesystem.Stat(to)
			if err != nil {
				return err
			}
			if !filesystem.Same(src, dst) {
				return fmt.Errorf("repair verification failed: %s", op.To)
			}
		} else {
			if err := filesystem.Link(op.Source, to); err != nil {
				return fmt.Errorf("%s: %w", op.To, err)
			}
		}
		if op.Kind == "MOVE_LINK" {
			from := filepath.Join(root, op.From)
			id, err := filesystem.Stat(from)
			if err != nil {
				return err
			}
			if !filesystem.Same(id, op.Expected) {
				return fmt.Errorf("source link changed during move: %s", op.From)
			}
			if err = os.Remove(from); err != nil {
				return err
			}
		}
		if log != nil {
			log(op)
		}
	}
	for _, op := range plan.Operations {
		if op.Kind != "REMOVE_LINK" {
			continue
		}
		from := filepath.Join(root, op.From)
		id, err := filesystem.Stat(from)
		if err != nil {
			return err
		}
		if !filesystem.Same(id, op.Expected) {
			return fmt.Errorf("managed link changed before removal: %s", op.From)
		}
		if err = os.Remove(from); err != nil {
			return err
		}
		if log != nil {
			log(op)
		}
	}
	return prune(root)
}

func prune(root string) error {
	// Only remove empty directories that match the generated date hierarchy.
	var dirs []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && path != root {
			dirs = append(dirs, path)
		}
		return nil
	})
	if err != nil {
		return err
	}
	for dirIndex := len(dirs) - 1; dirIndex >= 0; dirIndex-- {
		rel, err := filepath.Rel(root, dirs[dirIndex])
		if err != nil {
			return err
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) > 4 || len(parts) == 0 {
			continue
		}
		if len(parts) == 1 && !validYear(parts[0]) {
			continue
		}
		if len(parts) >= 2 && (len(parts[1]) != 7 || parts[1][:4] != parts[0] || parts[1][4] != '-' || !validYear(parts[0])) {
			continue
		}
		if len(parts) >= 2 {
			if _, err := time.Parse("2006-01", parts[1]); err != nil {
				continue
			}
		}
		if len(parts) == 4 && parts[3] != "orig" && parts[3] != "live" {
			continue
		}
		if len(parts) >= 3 && !isManagedLayout(filepath.Join(parts[0], parts[1], parts[2], "x")) {
			continue
		}
		_ = os.Remove(dirs[dirIndex])
	}
	return nil
}

func validYear(year string) bool {
	if len(year) != 4 {
		return false
	}
	for _, r := range year {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
