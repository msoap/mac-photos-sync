package sync

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/msoap/mac-photos-sync/internal/catalog"
	"github.com/msoap/mac-photos-sync/internal/filesystem"
	"github.com/msoap/mac-photos-sync/internal/model"
)

type Operation struct {
	Kind, From, To, Source, Detail string
	Expected                       filesystem.Identity
}
type Plan struct {
	Operations        []Operation
	Conflicts         []string
	Existing, Missing int
}

func dayPath(t time.Time) string {
	return filepath.Join(t.Format("2006"), t.Format("2006-01"), t.Format("2006-01-02"))
}
func sanitize(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		return "unnamed"
	}
	return strings.Map(func(char rune) rune {
		if char == '/' || char == '\\' || char == 0 || char < 32 || char == ':' {
			return '_'
		}
		return char
	}, name)
}
func suffix(name string, n int) string {
	ext := filepath.Ext(name)
	return strings.TrimSuffix(name, ext) + fmt.Sprintf("_%d", n) + ext
}
func isManagedLayout(rel string) bool {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) != 4 && len(parts) != 5 {
		return false
	}
	if len(parts) == 5 && parts[3] != "orig" && parts[3] != "live" {
		return false
	}
	if len(parts[0]) != 4 || len(parts[1]) != 7 || len(parts[2]) != 10 || parts[1][:4] != parts[0] || parts[2][:7] != parts[1] {
		return false
	}
	_, err := time.Parse("2006-01-02", parts[2])
	return err == nil
}
func scanDestination(root string) (map[string]filesystem.Identity, error) {
	out := make(map[string]filesystem.Identity)
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return out, nil
	} else if err != nil {
		return nil, err
	}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if !isManagedLayout(rel) {
			return nil
		}
		id, err := filesystem.Stat(path)
		if err == nil {
			out[rel] = id
		}
		return nil
	})
	return out, err
}

func Build(root string, assets []model.Asset, old catalog.State) (Plan, error) {
	var plan Plan
	existing, err := scanDestination(root)
	if err != nil {
		return plan, err
	}
	type item struct {
		ai, ri         int
		id             filesystem.Identity
		dir, name, key string
	}
	var items []item
	missingSources := make(map[string]bool)
	for ai := range assets {
		asset := &assets[ai]
		for ri := range asset.Resources {
			resource := &asset.Resources[ri]
			if resource.Missing {
				plan.Missing++
				missingSources[resource.SourcePath] = true
				continue
			}
			id, err := filesystem.Stat(resource.SourcePath)
			if err != nil {
				resource.Missing = true
				plan.Missing++
				missingSources[resource.SourcePath] = true
				continue
			}
			resource.Size = id.Size
			resource.Device = id.Device
			resource.Inode = id.Inode
			if asset.CaptureTime.IsZero() {
				fi, _ := os.Stat(resource.SourcePath)
				asset.CaptureTime = fi.ModTime()
				asset.CaptureTimeSource = "filesystem"
			}
			dir := dayPath(asset.CaptureTime)
			if resource.Type == "original" && hasAdjusted(asset.Resources) {
				dir = filepath.Join(dir, "orig")
			}
			if resource.Type == "live_photo_video" {
				dir = filepath.Join(dir, "live")
			}
			if resource.Type == "original_live_photo_video" {
				dir = filepath.Join(dir, "orig")
			}
			items = append(items, item{ai, ri, id, dir, sanitize(resource.OriginalFilename), asset.UUID + "|" + resource.Type + "|" + resource.SourcePath})
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].key < items[j].key })
	claimed := make(map[string]bool)
	// Existing assignments retain their suffix when the underlying resource is unchanged.
	for _, it := range items {
		resource := &assets[it.ai].Resources[it.ri]
		for path, rec := range old.Resources {
			if rec.AssetUUID != resource.AssetUUID || rec.Type != resource.Type || rec.SourcePath != resource.SourcePath || filepath.Dir(path) != it.dir || claimed[path] {
				continue
			}
			if id, ok := existing[path]; ok && filesystem.Same(id, it.id) {
				resource.DestinationPath = path
				resource.DestinationFilename = filepath.Base(path)
				claimed[path] = true
				break
			}
		}
	}
	for _, it := range items {
		resource := &assets[it.ai].Resources[it.ri]
		if resource.DestinationPath != "" {
			continue
		}
		for n := 1; ; n++ {
			name := it.name
			if n > 1 {
				name = suffix(name, n)
			}
			path := filepath.Join(it.dir, name)
			if claimed[path] {
				continue
			}
			resource.DestinationPath = path
			resource.DestinationFilename = name
			claimed[path] = true
			break
		}
	}
	usedOld := make(map[string]bool)
	for _, it := range items {
		resource := &assets[it.ai].Resources[it.ri]
		dest := resource.DestinationPath
		if id, ok := existing[dest]; ok {
			if filesystem.Same(id, it.id) {
				plan.Existing++
				usedOld[dest] = true
				continue
			}
			rec, managed := old.Resources[dest]
			if !managed || missingSources[rec.SourcePath] || id.Device != rec.Device || id.Inode != rec.Inode {
				plan.Conflicts = append(plan.Conflicts, "unrelated file at "+dest)
				continue
			}
			plan.Operations = append(plan.Operations, Operation{Kind: "REPAIR_LINK", To: dest, Source: resource.SourcePath, Expected: id})
			usedOld[dest] = true
			continue
		}
		var oldPath string
		for path, id := range existing {
			if usedOld[path] || claimed[path] || !filesystem.Same(id, it.id) {
				continue
			}
			oldPath = path
			break
		}
		if oldPath != "" {
			usedOld[oldPath] = true
			plan.Operations = append(plan.Operations, Operation{Kind: "MOVE_LINK", From: oldPath, To: dest, Source: resource.SourcePath, Expected: it.id})
			continue
		}
		plan.Operations = append(plan.Operations, Operation{Kind: "CREATE_LINK", To: dest, Source: resource.SourcePath})
	}
	for path, rec := range old.Resources {
		if usedOld[path] || claimed[path] {
			continue
		}
		if missingSources[rec.SourcePath] {
			continue
		}
		id, exists := existing[path]
		if !exists {
			continue
		}
		if id.Device == rec.Device && id.Inode == rec.Inode {
			plan.Operations = append(plan.Operations, Operation{Kind: "REMOVE_LINK", From: path, Expected: id})
		} else {
			plan.Conflicts = append(plan.Conflicts, "changed managed file at "+path)
		}
	}
	for path, id := range existing {
		name := filepath.Base(path)
		if claimed[path] || usedOld[path] || (!strings.HasPrefix(name, ".mac-photos-sync-") && !strings.HasPrefix(name, ".photos-sync-")) {
			continue
		}
		for _, it := range items {
			if filesystem.Same(id, it.id) {
				plan.Operations = append(plan.Operations, Operation{Kind: "REMOVE_LINK", From: path, Expected: id})
				break
			}
		}
	}
	sort.Slice(plan.Operations, func(i, j int) bool {
		left, right := plan.Operations[i], plan.Operations[j]
		if left.Kind != right.Kind {
			return left.Kind < right.Kind
		}
		return left.To+left.From < right.To+right.From
	})
	sort.Strings(plan.Conflicts)
	return plan, nil
}
func hasAdjusted(rs []model.Resource) bool {
	for _, r := range rs {
		if r.Type == "adjusted" {
			return true
		}
	}
	return false
}
