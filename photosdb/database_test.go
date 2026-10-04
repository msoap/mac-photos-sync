package photosdb

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestImmutableReadDoesNotTouchSHM(test *testing.T) {
	root := test.TempDir()
	dir := filepath.Join(root, "database")
	if err := os.MkdirAll(dir, 0755); err != nil {
		test.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "Photos.sqlite"))
	if err != nil {
		test.Fatal(err)
	}
	defer db.Close()
	for _, q := range []string{"PRAGMA journal_mode=WAL", "CREATE TABLE ZASSET(Z_PK INTEGER,ZUUID TEXT,ZDIRECTORY TEXT,ZFILENAME TEXT,ZDATECREATED REAL,ZKIND INTEGER,ZADJUSTMENTSSTATE INTEGER,ZWIDTH INTEGER,ZHEIGHT INTEGER,ZDURATION REAL,ZLATITUDE REAL,ZLONGITUDE REAL,ZFAVORITE INTEGER,ZHIDDEN INTEGER,ZTRASHEDSTATE INTEGER)", "CREATE TABLE ZADDITIONALASSETATTRIBUTES(ZASSET INTEGER,ZORIGINALFILENAME TEXT,ZTIMEZONEOFFSET INTEGER,ZINFERREDTIMEZONEOFFSET INTEGER)", "CREATE TABLE ZINTERNALRESOURCE(ZASSET INTEGER,ZRESOURCETYPE INTEGER,ZVERSION INTEGER,ZDATALENGTH INTEGER)", "INSERT INTO ZASSET VALUES(1,'AAAA','A','AAAA.jpeg',0,0,0,1,1,0,NULL,NULL,0,0,0)"} {
		if _, err = db.Exec(q); err != nil {
			test.Fatal(err)
		}
	}
	shm := filepath.Join(dir, "Photos.sqlite-shm")
	before, err := os.Stat(shm)
	if err != nil {
		test.Fatal(err)
	}
	libraryReader, err := Open(root)
	if err != nil {
		test.Fatal(err)
	}
	state, err := libraryReader.Scan(context.Background())
	libraryReader.Close()
	if err != nil {
		test.Fatal(err)
	}
	if len(state.Assets) != 1 {
		test.Fatalf("assets: %d", len(state.Assets))
	}
	after, err := os.Stat(shm)
	if err != nil {
		test.Fatal(err)
	}
	if before.ModTime() != after.ModTime() || before.Size() != after.Size() {
		test.Fatal("source SHM changed during read")
	}
}
