package catalog

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/msoap/mac-photos-sync/internal/model"
	_ "modernc.org/sqlite"
)

type Record struct {
	AssetUUID, Type, SourcePath, DestinationPath, OriginalFilename, DestinationFilename string
	Size                                                                                int64
	Device, Inode                                                                       uint64
	Metadata                                                                            model.Metadata
}
type State struct {
	Resources map[string]Record
	Exists    bool
}

func Read(root string) (State, error) {
	state := State{Resources: make(map[string]Record)}
	dbPath := filepath.Join(root, "photos.sqlite")
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return state, nil
	} else if err != nil {
		return state, err
	}
	dbURL := url.URL{Scheme: "file", Path: dbPath, RawQuery: "mode=ro"}
	db, err := sql.Open("sqlite", dbURL.String())
	if err != nil {
		return state, err
	}
	defer db.Close()
	rows, err := db.Query(`SELECT a.photos_uuid,r.resource_type,r.source_path,r.destination_path,r.original_filename,r.destination_filename,r.size,r.device,r.inode,r.metadata_json FROM resources r JOIN assets a ON a.id=r.asset_id`)
	if err != nil {
		return state, err
	}
	defer rows.Close()
	for rows.Next() {
		var record Record
		var raw sql.NullString
		if err = rows.Scan(&record.AssetUUID, &record.Type, &record.SourcePath, &record.DestinationPath, &record.OriginalFilename, &record.DestinationFilename, &record.Size, &record.Device, &record.Inode, &raw); err != nil {
			return state, err
		}
		if raw.Valid {
			_ = json.Unmarshal([]byte(raw.String), &record.Metadata)
		}
		state.Resources[record.DestinationPath] = record
	}
	if err = rows.Err(); err != nil {
		return state, err
	}
	state.Exists = true
	return state, nil
}

const schema = `PRAGMA foreign_keys=ON;
CREATE TABLE IF NOT EXISTS assets(id INTEGER PRIMARY KEY,photos_uuid TEXT NOT NULL UNIQUE,original_filename TEXT,filename_source TEXT,capture_time TEXT,capture_time_source TEXT,timezone_offset INTEGER,media_type TEXT,width INTEGER,height INTEGER,duration_ms INTEGER,latitude REAL,longitude REAL,favorite INTEGER,hidden INTEGER,metadata_json TEXT,synced_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS resources(id INTEGER PRIMARY KEY,asset_id INTEGER NOT NULL,resource_type TEXT NOT NULL,source_path TEXT NOT NULL,destination_path TEXT NOT NULL UNIQUE,original_filename TEXT,destination_filename TEXT,size INTEGER,device INTEGER,inode INTEGER,metadata_json TEXT,FOREIGN KEY(asset_id) REFERENCES assets(id));
CREATE TABLE IF NOT EXISTS sync_info(key TEXT PRIMARY KEY,value TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS idx_resources_asset ON resources(asset_id);
CREATE INDEX IF NOT EXISTS idx_resources_inode ON resources(device,inode);
CREATE INDEX IF NOT EXISTS idx_assets_capture_time ON assets(capture_time);`

func Write(root string, assets []model.Asset) error {
	tmp, err := os.CreateTemp(root, ".mac-photos-sync-catalog-*.sqlite")
	if err != nil {
		return err
	}
	tempPath := tmp.Name()
	if err = tmp.Close(); err != nil {
		return err
	}
	defer os.Remove(tempPath)
	db, err := sql.Open("sqlite", tempPath)
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err = db.Exec(schema); err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("DELETE FROM resources"); err != nil {
		return err
	}
	if _, err = tx.Exec("DELETE FROM assets"); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, asset := range assets {
		meta, err := json.Marshal(asset.Metadata)
		if err != nil {
			return fmt.Errorf("asset metadata %s: %w", asset.UUID, err)
		}
		var lat, lon any
		if asset.Latitude != nil {
			lat = *asset.Latitude
		}
		if asset.Longitude != nil {
			lon = *asset.Longitude
		}
		res, err := tx.Exec(`INSERT INTO assets(photos_uuid,original_filename,filename_source,capture_time,capture_time_source,timezone_offset,media_type,width,height,duration_ms,latitude,longitude,favorite,hidden,metadata_json,synced_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, asset.UUID, asset.OriginalFilename, asset.FilenameSource, asset.CaptureTime.Format(time.RFC3339Nano), asset.CaptureTimeSource, asset.TimezoneOffset, asset.MediaType, asset.Width, asset.Height, asset.DurationMS, lat, lon, asset.Favorite, asset.Hidden, string(meta), now)
		if err != nil {
			return fmt.Errorf("asset %s: %w", asset.UUID, err)
		}
		id, _ := res.LastInsertId()
		for _, resource := range asset.Resources {
			if resource.Missing || resource.DestinationPath == "" {
				continue
			}
			raw, err := json.Marshal(resource.Metadata)
			if err != nil {
				return fmt.Errorf("resource metadata %s: %w", resource.SourcePath, err)
			}
			if _, err = tx.Exec(`INSERT INTO resources(asset_id,resource_type,source_path,destination_path,original_filename,destination_filename,size,device,inode,metadata_json) VALUES(?,?,?,?,?,?,?,?,?,?)`, id, resource.Type, resource.SourcePath, resource.DestinationPath, resource.OriginalFilename, resource.DestinationFilename, resource.Size, resource.Device, resource.Inode, string(raw)); err != nil {
				return fmt.Errorf("resource %s: %w", resource.DestinationPath, err)
			}
		}
	}
	if _, err = tx.Exec("INSERT OR REPLACE INTO sync_info(key,value) VALUES('synced_at',?)", now); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	if err = db.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, filepath.Join(root, "photos.sqlite"))
}
