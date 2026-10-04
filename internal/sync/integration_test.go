package sync

import (
	"bytes"
	"context"
	"database/sql"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"

	"github.com/msoap/mac-photos-sync/internal/filesystem"
	_ "modernc.org/sqlite"
)

func TestSyntheticLibraryLifecycle(test *testing.T) {
	lib := filepath.Join(test.TempDir(), "Photos Library.photoslibrary")
	dest := filepath.Join(test.TempDir(), "mirror")
	os.MkdirAll(filepath.Join(lib, "database"), 0755)
	os.MkdirAll(filepath.Join(lib, "originals", "A"), 0755)
	os.MkdirAll(filepath.Join(lib, "originals", "B"), 0755)
	os.MkdirAll(filepath.Join(lib, "originals", "C"), 0755)
	os.MkdirAll(filepath.Join(lib, "resources", "renders", "A"), 0755)
	os.MkdirAll(filepath.Join(lib, "resources", "renders", "C"), 0755)
	makeJPEG := func(path string) int64 {
		file, err := os.Create(path)
		if err != nil {
			test.Fatal(err)
		}
		img := image.NewRGBA(image.Rect(0, 0, 2, 2))
		img.Set(0, 0, color.White)
		if err = jpeg.Encode(file, img, nil); err != nil {
			test.Fatal(err)
		}
		file.Close()
		fi, _ := os.Stat(path)
		return fi.Size()
	}
	baseA := filepath.Join(lib, "originals", "A", "AAAA.jpeg")
	baseB := filepath.Join(lib, "originals", "B", "BBBB.jpeg")
	render := filepath.Join(lib, "resources", "renders", "A", "AAAA_1_201_a.jpeg")
	live := filepath.Join(lib, "originals", "A", "AAAA_3.mov")
	liveRender := filepath.Join(lib, "resources", "renders", "A", "AAAA_2_100_a.mov")
	video := filepath.Join(lib, "originals", "C", "CCCC.mov")
	videoRender := filepath.Join(lib, "resources", "renders", "C", "CCCC_2_0_a.mov")
	sa := makeJPEG(baseA)
	sb := makeJPEG(baseB)
	sr := makeJPEG(render)
	os.WriteFile(live, []byte("fake-mov"), 0600)
	os.WriteFile(liveRender, []byte("edited-mov"), 0600)
	os.WriteFile(video, []byte("video-original"), 0600)
	os.WriteFile(videoRender, []byte("video-edited"), 0600)
	db, err := sql.Open("sqlite", filepath.Join(lib, "database", "Photos.sqlite"))
	if err != nil {
		test.Fatal(err)
	}
	for _, query := range []string{
		`CREATE TABLE ZASSET(Z_PK INTEGER,ZUUID TEXT,ZDIRECTORY TEXT,ZFILENAME TEXT,ZDATECREATED REAL,ZKIND INTEGER,ZADJUSTMENTSSTATE INTEGER,ZWIDTH INTEGER,ZHEIGHT INTEGER,ZDURATION REAL,ZLATITUDE REAL,ZLONGITUDE REAL,ZFAVORITE INTEGER,ZHIDDEN INTEGER,ZTRASHEDSTATE INTEGER)`,
		`CREATE TABLE ZADDITIONALASSETATTRIBUTES(ZASSET INTEGER,ZORIGINALFILENAME TEXT,ZTIMEZONEOFFSET INTEGER,ZINFERREDTIMEZONEOFFSET INTEGER)`,
		`CREATE TABLE ZINTERNALRESOURCE(ZASSET INTEGER,ZRESOURCETYPE INTEGER,ZVERSION INTEGER,ZDATALENGTH INTEGER)`,
		`INSERT INTO ZASSET VALUES(1,'AAAA','A','AAAA.jpeg',731640000,0,2,2,2,0,NULL,NULL,0,0,0)`,
		`INSERT INTO ZASSET VALUES(2,'BBBB','B','BBBB.jpeg',731640000,0,0,2,2,0,NULL,NULL,0,0,0)`,
		`INSERT INTO ZASSET VALUES(3,'CCCC','C','CCCC.mov',731640000,1,2,2,2,0,NULL,NULL,0,0,0)`,
		`INSERT INTO ZADDITIONALASSETATTRIBUTES VALUES(1,'IMG_0001.JPG',0,0)`,
		`INSERT INTO ZADDITIONALASSETATTRIBUTES VALUES(2,'IMG_0001.JPG',0,0)`,
		`INSERT INTO ZADDITIONALASSETATTRIBUTES VALUES(3,'CLIP.MOV',0,0)`,
	} {
		if _, err = db.Exec(query); err != nil {
			test.Fatal(err)
		}
	}
	for _, resourceRow := range []struct {
		asset, typ, version int
		size                int64
	}{{1, 0, 0, sa}, {1, 0, 2, sr}, {1, 3, 0, 8}, {1, 3, 2, 10}, {2, 0, 0, sb}, {3, 1, 0, 14}, {3, 1, 2, 12}} {
		if _, err = db.Exec(`INSERT INTO ZINTERNALRESOURCE VALUES(?,?,?,?)`, resourceRow.asset, resourceRow.typ, resourceRow.version, resourceRow.size); err != nil {
			test.Fatal(err)
		}
	}
	run := func(dry bool) Summary {
		test.Helper()
		var b bytes.Buffer
		result, err := Run(context.Background(), Options{Library: lib, Destination: dest, DryRun: dry, Workers: 2, Output: &b})
		if err != nil {
			test.Fatalf("run: %v\n%s", err, b.String())
		}
		return result
	}
	if s := run(true); s.Created != 7 {
		test.Fatalf("dry plan: %+v", s)
	}
	if _, err = os.Stat(dest); !os.IsNotExist(err) {
		test.Fatal("dry run created destination")
	}
	if s := run(false); s.Created != 7 {
		test.Fatalf("first sync: %+v", s)
	}
	if s := run(false); s.Created+s.Moved+s.Removed+s.Repaired != 0 {
		test.Fatalf("repeat mutated media: %+v", s)
	}
	check := func(src, rel string) {
		test.Helper()
		a, _ := filesystem.Stat(src)
		b, err := filesystem.Stat(filepath.Join(dest, rel))
		if err != nil || !filesystem.Same(a, b) {
			test.Fatalf("bad link %s: %v", rel, err)
		}
	}
	// Read the actual date path from the catalog to avoid tying this test to the literal date.
	rows, err := sql.Open("sqlite", filepath.Join(dest, "photos.sqlite"))
	if err != nil {
		test.Fatal(err)
	}
	var rel string
	canonicalRender, _ := filepath.EvalSymlinks(render)
	if err = rows.QueryRow(`SELECT destination_path FROM resources WHERE source_path=?`, canonicalRender).Scan(&rel); err != nil {
		test.Fatal(err)
	}
	rows.Close()
	check(render, rel)
	dir := filepath.Dir(rel)
	check(baseA, filepath.Join(dir, "orig", "IMG_0001.JPG"))
	check(liveRender, filepath.Join(dir, "live", "IMG_0001.MOV"))
	check(live, filepath.Join(dir, "orig", "IMG_0001.MOV"))
	check(videoRender, filepath.Join(dir, "CLIP.MOV"))
	check(video, filepath.Join(dir, "orig", "CLIP.MOV"))
	if _, err = db.Exec(`DELETE FROM ZASSET WHERE Z_PK=2`); err != nil {
		test.Fatal(err)
	}
	if s := run(false); s.Removed != 1 {
		test.Fatalf("removed asset: %+v", s)
	}
	if _, err = db.Exec(`UPDATE ZASSET SET ZDATECREATED=ZDATECREATED+86400 WHERE Z_PK=1`); err != nil {
		test.Fatal(err)
	}
	if s := run(false); s.Moved != 4 {
		test.Fatalf("date change: %+v", s)
	}
	if err = os.Remove(filepath.Join(dest, "photos.sqlite")); err != nil {
		test.Fatal(err)
	}
	if s := run(false); s.Created+s.Moved+s.Removed+s.Repaired != 0 {
		test.Fatalf("rebuild: %+v", s)
	}
	db.Close()
}
