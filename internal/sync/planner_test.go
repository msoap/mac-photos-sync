package sync

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/msoap/mac-photos-sync/internal/catalog"
	"github.com/msoap/mac-photos-sync/internal/filesystem"
	"github.com/msoap/mac-photos-sync/internal/model"
)

func TestPathAndSanitize(test *testing.T) {
	if got := dayPath(time.Date(2024, 3, 18, 12, 0, 0, 0, time.UTC)); got != filepath.Join("2024", "2024-03", "2024-03-18") {
		test.Fatal(got)
	}
	if got := sanitize("../bad:name.JPG"); got != "bad_name.JPG" {
		test.Fatal(got)
	}
	if got := suffix("IMG_0001.JPG", 2); got != "IMG_0001_2.JPG" {
		test.Fatal(got)
	}
}

func TestBuildCollisionAndPlacement(test *testing.T) {
	root := test.TempDir()
	source := test.TempDir()
	date := time.Date(2024, 3, 18, 12, 0, 0, 0, time.UTC)
	makeFile := func(n string) string {
		path := filepath.Join(source, n)
		if err := os.WriteFile(path, []byte(n), 0600); err != nil {
			test.Fatal(err)
		}
		return path
	}
	assets := []model.Asset{
		{UUID: "a", CaptureTime: date, Resources: []model.Resource{{AssetUUID: "a", Type: "adjusted", SourcePath: makeFile("edit.jpg"), OriginalFilename: "IMG_0001.JPG"}, {AssetUUID: "a", Type: "original", SourcePath: makeFile("orig.jpg"), OriginalFilename: "IMG_0001.JPG"}, {AssetUUID: "a", Type: "live_photo_video", SourcePath: makeFile("live.mov"), OriginalFilename: "IMG_0001.MOV"}}},
		{UUID: "b", CaptureTime: date, Resources: []model.Resource{{AssetUUID: "b", Type: "original", SourcePath: makeFile("other.jpg"), OriginalFilename: "IMG_0001.JPG"}}},
	}
	plan, err := Build(root, assets, catalog.State{Resources: map[string]catalog.Record{}})
	if err != nil {
		test.Fatal(err)
	}
	if len(plan.Operations) != 4 {
		test.Fatalf("operations: %+v", plan.Operations)
	}
	for _, want := range []string{"2024/2024-03/2024-03-18/IMG_0001.JPG", "2024/2024-03/2024-03-18/IMG_0001_2.JPG", "2024/2024-03/2024-03-18/orig/IMG_0001.JPG", "2024/2024-03/2024-03-18/live/IMG_0001.MOV"} {
		found := false
		for _, asset := range assets {
			for _, r := range asset.Resources {
				if filepath.ToSlash(r.DestinationPath) == want {
					found = true
				}
			}
		}
		if !found {
			test.Errorf("missing %s", want)
		}
	}
	if err = Apply(root, plan, nil); err != nil {
		test.Fatal(err)
	}
	plan, err = Build(root, assets, catalog.State{Resources: map[string]catalog.Record{}})
	if err != nil {
		test.Fatal(err)
	}
	if len(plan.Operations) != 0 {
		test.Fatalf("second run: %+v", plan.Operations)
	}
	for _, asset := range assets {
		for _, resource := range asset.Resources {
			src, _ := filesystem.Stat(resource.SourcePath)
			dst, _ := filesystem.Stat(filepath.Join(root, resource.DestinationPath))
			if !filesystem.Same(src, dst) {
				test.Fatal("not hard linked", resource.DestinationPath)
			}
		}
	}
}

func TestMoveRemovalAndForeignFile(test *testing.T) {
	root := test.TempDir()
	source := filepath.Join(test.TempDir(), "source.jpg")
	os.WriteFile(source, []byte("photo"), 0600)
	oldPath := filepath.Join("2024", "2024-03", "2024-03-18", "photo.jpg")
	os.MkdirAll(filepath.Join(root, filepath.Dir(oldPath)), 0755)
	if err := os.Link(source, filepath.Join(root, oldPath)); err != nil {
		test.Fatal(err)
	}
	id, _ := filesystem.Stat(source)
	old := catalog.State{Resources: map[string]catalog.Record{oldPath: {AssetUUID: "a", Type: "original", SourcePath: source, DestinationPath: oldPath, Device: id.Device, Inode: id.Inode, Size: id.Size}}}
	assets := []model.Asset{{UUID: "a", CaptureTime: time.Date(2024, 3, 19, 0, 0, 0, 0, time.UTC), Resources: []model.Resource{{AssetUUID: "a", Type: "original", SourcePath: source, OriginalFilename: "photo.jpg"}}}}
	plan, err := Build(root, assets, old)
	if err != nil {
		test.Fatal(err)
	}
	if len(plan.Operations) != 1 || plan.Operations[0].Kind != "MOVE_LINK" {
		test.Fatalf("move: %+v", plan.Operations)
	}
	if err = Apply(root, plan, nil); err != nil {
		test.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(root, oldPath)); !os.IsNotExist(err) {
		test.Fatal("old link remains")
	}
	newPath := assets[0].Resources[0].DestinationPath
	old.Resources = map[string]catalog.Record{newPath: {AssetUUID: "a", Type: "original", SourcePath: source, DestinationPath: newPath, Device: id.Device, Inode: id.Inode, Size: id.Size}}
	foreign := filepath.Join(root, "2024", "2024-03", "2024-03-19", "notes.txt")
	os.WriteFile(foreign, []byte("keep"), 0600)
	plan, err = Build(root, nil, old)
	if err != nil {
		test.Fatal(err)
	}
	if len(plan.Operations) != 1 || plan.Operations[0].Kind != "REMOVE_LINK" {
		test.Fatalf("remove: %+v", plan.Operations)
	}
	if err = Apply(root, plan, nil); err != nil {
		test.Fatal(err)
	}
	if _, err = os.Stat(foreign); err != nil {
		test.Fatal("foreign file removed", err)
	}
}

func TestMissingSourceRetainsExistingLink(test *testing.T) {
	root := test.TempDir()
	src := filepath.Join(test.TempDir(), "source.jpg")
	if err := os.WriteFile(src, []byte("photo"), 0600); err != nil {
		test.Fatal(err)
	}
	rel := filepath.Join("2024", "2024-03", "2024-03-18", "photo.jpg")
	os.MkdirAll(filepath.Join(root, filepath.Dir(rel)), 0755)
	if err := os.Link(src, filepath.Join(root, rel)); err != nil {
		test.Fatal(err)
	}
	id, _ := filesystem.Stat(src)
	if err := os.Remove(src); err != nil {
		test.Fatal(err)
	}
	old := catalog.State{Resources: map[string]catalog.Record{rel: {AssetUUID: "a", Type: "original", SourcePath: src, DestinationPath: rel, Device: id.Device, Inode: id.Inode, Size: id.Size}}}
	a := []model.Asset{{UUID: "a", Resources: []model.Resource{{AssetUUID: "a", Type: "original", SourcePath: src, OriginalFilename: "photo.jpg", Missing: true}}}}
	plan, err := Build(root, a, old)
	if err != nil {
		test.Fatal(err)
	}
	if plan.Missing != 1 || len(plan.Operations) != 0 {
		test.Fatalf("missing source: %+v", plan)
	}
}

func TestForeignConflictAndManagedRepair(test *testing.T) {
	root := test.TempDir()
	src := filepath.Join(test.TempDir(), "source.jpg")
	if err := os.WriteFile(src, []byte("source"), 0600); err != nil {
		test.Fatal(err)
	}
	date := time.Date(2024, 3, 18, 0, 0, 0, 0, time.UTC)
	rel := filepath.Join(dayPath(date), "photo.jpg")
	dst := filepath.Join(root, rel)
	os.MkdirAll(filepath.Dir(dst), 0755)
	if err := os.WriteFile(dst, []byte("foreign"), 0600); err != nil {
		test.Fatal(err)
	}
	assets := []model.Asset{{UUID: "a", CaptureTime: date, Resources: []model.Resource{{AssetUUID: "a", Type: "original", SourcePath: src, OriginalFilename: "photo.jpg"}}}}
	plan, err := Build(root, assets, catalog.State{Resources: map[string]catalog.Record{}})
	if err != nil {
		test.Fatal(err)
	}
	if len(plan.Conflicts) != 1 || len(plan.Operations) != 0 {
		test.Fatalf("foreign file: %+v", plan)
	}
	data, _ := os.ReadFile(dst)
	if string(data) != "foreign" {
		test.Fatal("foreign file modified")
	}
	id, _ := filesystem.Stat(dst)
	old := catalog.State{Resources: map[string]catalog.Record{rel: {AssetUUID: "a", Type: "original", SourcePath: src, DestinationPath: rel, Device: id.Device, Inode: id.Inode, Size: id.Size}}}
	plan, err = Build(root, assets, old)
	if err != nil {
		test.Fatal(err)
	}
	if len(plan.Operations) != 1 || plan.Operations[0].Kind != "REPAIR_LINK" {
		test.Fatalf("repair: %+v", plan)
	}
	if err = Apply(root, plan, nil); err != nil {
		test.Fatal(err)
	}
	sid, _ := filesystem.Stat(src)
	did, _ := filesystem.Stat(dst)
	if !filesystem.Same(sid, did) {
		test.Fatal("repair did not link source")
	}
}
