package photosdb

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/msoap/mac-photos-sync/model"
	_ "modernc.org/sqlite"
)

type Library struct {
	Root          string
	db            *sql.DB
	tempDir       string
	SchemaVersion int
}

type ScanResult struct {
	Assets  []model.Asset
	Missing []string
}

func Open(root string) (*Library, error) {
	version, err := Version(root)
	if err != nil {
		return nil, err
	}
	if version != 0 && version != 5001 {
		return nil, fmt.Errorf("unsupported Photos library schema version %d (supported: 5001)", version)
	}
	tempDir, err := snapshot(root)
	if err != nil {
		return nil, err
	}
	dbPath := filepath.Join(tempDir, "Photos.sqlite")
	dbURL := url.URL{Scheme: "file", Path: dbPath, RawQuery: "mode=ro"}
	db, err := sql.Open("sqlite", dbURL.String())
	if err != nil {
		os.RemoveAll(tempDir)
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err = db.Ping(); err != nil {
		db.Close()
		os.RemoveAll(tempDir)
		return nil, err
	}
	for _, tableName := range []string{"ZASSET", "ZADDITIONALASSETATTRIBUTES", "ZINTERNALRESOURCE"} {
		var tableCount int
		if err = db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?", tableName).Scan(&tableCount); err != nil || tableCount != 1 {
			db.Close()
			os.RemoveAll(tempDir)
			return nil, fmt.Errorf("unsupported Photos database schema: missing %s", tableName)
		}
	}
	return &Library{Root: root, db: db, tempDir: tempDir, SchemaVersion: version}, nil
}

func (library *Library) Close() error {
	err := library.db.Close()
	cleanup := os.RemoveAll(library.tempDir)
	if err != nil {
		return err
	}
	return cleanup
}

func snapshot(root string) (string, error) {
	srcDir := filepath.Join(root, "database")
	tempDir, err := os.MkdirTemp("", "mac-photos-sync-db-")
	if err != nil {
		return "", err
	}
	clean := func(err error) (string, error) { os.RemoveAll(tempDir); return "", err }
	names := []string{"Photos.sqlite", "Photos.sqlite-wal"}
	before := make(map[string]os.FileInfo)
	for _, name := range names {
		fi, err := os.Stat(filepath.Join(srcDir, name))
		if os.IsNotExist(err) && name == "Photos.sqlite-wal" {
			continue
		}
		if err != nil {
			return clean(err)
		}
		before[name] = fi
	}
	for _, name := range names {
		if before[name] == nil {
			continue
		}
		src := filepath.Join(srcDir, name)
		in, err := os.Open(src)
		if err != nil {
			return clean(err)
		}
		out, err := os.OpenFile(filepath.Join(tempDir, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			in.Close()
			return clean(err)
		}
		_, err = io.Copy(out, in)
		closeOut := out.Close()
		closeIn := in.Close()
		if err != nil {
			return clean(err)
		}
		if closeOut != nil {
			return clean(closeOut)
		}
		if closeIn != nil {
			return clean(closeIn)
		}
	}
	for _, name := range names {
		after, err := os.Stat(filepath.Join(srcDir, name))
		if os.IsNotExist(err) && before[name] == nil {
			continue
		}
		if err != nil {
			return clean(err)
		}
		fi := before[name]
		if fi == nil || !os.SameFile(fi, after) || fi.Size() != after.Size() || !fi.ModTime().Equal(after.ModTime()) {
			return clean(fmt.Errorf("Photos database changed during snapshot; close Photos.app and retry"))
		}
	}
	return tempDir, nil
}

type resourceRow struct {
	typ, version int
	size         int64
}

func (library *Library) Scan(ctx context.Context) (ScanResult, error) {
	var result ScanResult
	resourceMap, err := library.allResources(ctx)
	if err != nil {
		return result, err
	}
	rows, err := library.db.QueryContext(ctx, `SELECT a.Z_PK,a.ZUUID,a.ZDIRECTORY,a.ZFILENAME,COALESCE(x.ZORIGINALFILENAME,''),a.ZDATECREATED,
	 COALESCE(x.ZTIMEZONEOFFSET,x.ZINFERREDTIMEZONEOFFSET,0),a.ZKIND,a.ZADJUSTMENTSSTATE,a.ZWIDTH,a.ZHEIGHT,
	 COALESCE(a.ZDURATION,0),a.ZLATITUDE,a.ZLONGITUDE,a.ZFAVORITE,a.ZHIDDEN
	 FROM ZASSET a LEFT JOIN ZADDITIONALASSETATTRIBUTES x ON x.ZASSET=a.Z_PK
	 WHERE COALESCE(a.ZTRASHEDSTATE,0)=0 ORDER BY a.ZUUID`)
	if err != nil {
		return result, fmt.Errorf("Photos asset query: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var pk, kind, adjust, width, height, fav, hid, offset int
		var uuid, dir, filename, original string
		var date, dur, lat, lon sql.NullFloat64
		if err = rows.Scan(&pk, &uuid, &dir, &filename, &original, &date, &offset, &kind, &adjust, &width, &height, &dur, &lat, &lon, &fav, &hid); err != nil {
			return result, err
		}
		if len(dir) != 1 || !strings.ContainsRune("0123456789ABCDEF", rune(dir[0])) || filepath.Base(filename) != filename || !strings.HasPrefix(filename, uuid) {
			return result, fmt.Errorf("unsupported Photos asset path for %s", uuid)
		}
		asset := model.Asset{UUID: uuid, OriginalFilename: original, FilenameSource: "photos_database", TimezoneOffset: offset, Width: width, Height: height, Favorite: fav != 0, Hidden: hid != 0}
		if kind == 1 {
			asset.MediaType = "video"
		} else {
			asset.MediaType = "photo"
		}
		if dur.Valid {
			asset.DurationMS = int64(dur.Float64 * 1000)
		}
		if lat.Valid && lon.Valid {
			asset.Latitude = &lat.Float64
			asset.Longitude = &lon.Float64
		}
		if !date.Valid || date.Float64 == 0 {
			asset.CaptureTimeSource = "filesystem"
		} else {
			asset.CaptureTime = AppleTime(date.Float64).In(time.FixedZone("Photos", offset))
			asset.CaptureTimeSource = "photos_database"
		}
		if original == "" {
			asset.OriginalFilename = filename
			asset.FilenameSource = "filesystem"
		}
		asset.Resources = library.resolve(asset, dir, filename, adjust, resourceMap[pk], &result.Missing)
		result.Assets = append(result.Assets, asset)
	}
	return result, rows.Err()
}

func (library *Library) allResources(ctx context.Context) (map[int][]resourceRow, error) {
	rows, err := library.db.QueryContext(ctx, "SELECT ZASSET,ZRESOURCETYPE,ZVERSION,COALESCE(ZDATALENGTH,0) FROM ZINTERNALRESOURCE")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[int][]resourceRow)
	for rows.Next() {
		var pk int
		var resource resourceRow
		if err = rows.Scan(&pk, &resource.typ, &resource.version, &resource.size); err != nil {
			return nil, err
		}
		out[pk] = append(out[pk], resource)
	}
	return out, rows.Err()
}

func (library *Library) resolve(asset model.Asset, dir, filename string, adjust int, rr []resourceRow, missing *[]string) []model.Resource {
	origDir := filepath.Join(library.Root, "originals", dir)
	renderDir := filepath.Join(library.Root, "resources", "renders", dir)
	main := filepath.Join(origDir, filename)
	base := strings.TrimSuffix(filename, filepath.Ext(filename))
	var out []model.Resource
	add := func(typ, path, name string) {
		resource := model.Resource{AssetUUID: asset.UUID, Type: typ, SourcePath: path, OriginalFilename: name}
		if fi, err := os.Stat(path); err == nil && fi.Mode().IsRegular() {
			resource.Size = fi.Size()
		} else {
			resource.Missing = true
			*missing = append(*missing, path)
		}
		out = append(out, resource)
	}
	currentName := asset.OriginalFilename
	if adjust != 0 {
		for _, resourceRow := range rr {
			if ((asset.MediaType == "photo" && resourceRow.typ == 0) || (asset.MediaType == "video" && resourceRow.typ == 1)) && resourceRow.version == 2 {
				pattern := base + "_1_201_a.*"
				if resourceRow.typ == 1 {
					pattern = base + "_2_0_a.*"
				}
				if renderPath := matchSize(renderDir, pattern, resourceRow.size); renderPath != "" {
					if !compatibleExtension(filepath.Ext(currentName), filepath.Ext(renderPath)) {
						currentName = replaceExtension(currentName, strings.ToUpper(filepath.Ext(renderPath)))
					}
					add("adjusted", renderPath, currentName)
					break
				}
				missingRender := strings.TrimSuffix(pattern, ".*") + filepath.Ext(filename)
				add("adjusted", filepath.Join(renderDir, missingRender), currentName)
				break
			}
		}
	}
	if len(out) > 0 {
		add("original", main, asset.OriginalFilename)
	} else {
		add(primaryType(asset), main, asset.OriginalFilename)
	}
	adjustedLive := false
	for _, resourceRow := range rr {
		if resourceRow.typ == 3 && resourceRow.version == 2 {
			adjustedLive = true
			motionPath := matchSize(renderDir, base+"_2_100_a.*", resourceRow.size)
			if motionPath == "" {
				motionPath = filepath.Join(renderDir, base+"_2_100_a.mov")
			}
			add("live_photo_video", motionPath, replaceExtension(asset.OriginalFilename, strings.ToUpper(filepath.Ext(motionPath))))
			break
		}
	}
	// Other full-quality originals belong to this asset. Resource rows and exact
	// sizes identify Live Photo companions; sidecars and cache files are ignored.
	entries, _ := os.ReadDir(origDir)
	for _, entry := range entries {
		entryName := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(entryName, base+"_") {
			continue
		}
		sourcePath := filepath.Join(origDir, entryName)
		fi, err := entry.Info()
		if err != nil || !fi.Mode().IsRegular() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(entryName))
		if ext == ".aae" || ext == ".plist" {
			continue
		}
		live := false
		for _, resourceRow := range rr {
			if resourceRow.typ == 3 && resourceRow.version == 0 && resourceRow.size == fi.Size() {
				live = true
				break
			}
		}
		if live {
			name := replaceExtension(asset.OriginalFilename, strings.ToUpper(filepath.Ext(entryName)))
			if adjustedLive {
				add("original_live_photo_video", sourcePath, name)
			} else {
				add("live_photo_video", sourcePath, name)
			}
			continue
		}
		full := false
		for _, resourceRow := range rr {
			if resourceRow.version == 0 && resourceRow.size == fi.Size() && (resourceRow.typ == 0 || resourceRow.typ == 1) {
				full = true
				break
			}
		}
		if full {
			add("paired_image", sourcePath, replaceExtension(asset.OriginalFilename, filepath.Ext(entryName)))
		}
	}
	// Explicitly report a missing companion rather than silently omitting it.
	for _, resourceRow := range rr {
		if resourceRow.typ != 3 || resourceRow.version != 0 {
			continue
		}
		found := false
		for _, candidate := range out {
			if ((adjustedLive && candidate.Type == "original_live_photo_video") || (!adjustedLive && candidate.Type == "live_photo_video")) && candidate.Size == resourceRow.size {
				found = true
				break
			}
		}
		if !found {
			missingPath := filepath.Join(origDir, base+"_3.mov")
			if adjustedLive {
				add("original_live_photo_video", missingPath, replaceExtension(asset.OriginalFilename, ".MOV"))
			} else {
				add("live_photo_video", missingPath, replaceExtension(asset.OriginalFilename, ".MOV"))
			}
		}
	}
	sort.Slice(out, func(leftIndex, rightIndex int) bool {
		if out[leftIndex].Type == out[rightIndex].Type {
			return out[leftIndex].SourcePath < out[rightIndex].SourcePath
		}
		return out[leftIndex].Type < out[rightIndex].Type
	})
	return out
}

func primaryType(asset model.Asset) string {
	if asset.MediaType == "video" {
		return "video"
	}
	return "original"
}

func replaceExtension(name, ext string) string {
	return strings.TrimSuffix(name, filepath.Ext(name)) + ext
}

func compatibleExtension(originalExt, renderExt string) bool {
	originalExt = strings.ToLower(originalExt)
	renderExt = strings.ToLower(renderExt)
	if originalExt == renderExt {
		return true
	}
	return (originalExt == ".jpg" || originalExt == ".jpeg") && (renderExt == ".jpg" || renderExt == ".jpeg")
}

func matchSize(dir, pattern string, size int64) string {
	paths, _ := filepath.Glob(filepath.Join(dir, pattern))
	for _, path := range paths {
		fi, err := os.Stat(path)
		if err == nil && fi.Mode().IsRegular() && fi.Size() == size {
			return path
		}
	}
	return ""
}
