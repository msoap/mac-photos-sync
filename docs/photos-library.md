# Supplied Photos library investigation

The supplied package is `test_data/from/Photos Library.photoslibrary`. Its database is `database/Photos.sqlite`, with existing `Photos.sqlite-wal` and `Photos.sqlite-shm` files. `database/DataModelVersion.plist` reports `LibrarySchemaVersion = 5001` and `MetaSchemaVersion = 3`. The macOS and Photos application versions cannot be determined from these files. The database has 7,461 `ZASSET` rows; all have `ZTRASHEDSTATE = 0` in this sample.

## Schema and relationships

`ZASSET.Z_PK` joins to `ZADDITIONALASSETATTRIBUTES.ZASSET` and `ZINTERNALRESOURCE.ZASSET`. `ZASSET.ZUUID` identifies a logical asset. `ZDIRECTORY` is the first hex digit used under `originals/` and `resources/renders/`; `ZFILENAME` is the physical primary original filename. `ZDATECREATED` is seconds after 2001-01-01 UTC. `ZKIND = 0` denotes an image asset and `ZKIND = 1` a standalone video in this library. `ZTRASHEDSTATE = 0` denotes the live assets observed here; synchronization excludes other values.

`ZADDITIONALASSETATTRIBUTES.ZORIGINALFILENAME` contains the imported filename. `ZTIMEZONEOFFSET` and `ZINFERREDTIMEZONEOFFSET` contain seconds east of UTC, used to choose the capture-date directory. The additional attributes also contain `ZTIMEZONENAME`, `ZEXIFTIMESTAMPSTRING`, and the original file size.

`ZINTERNALRESOURCE` identifies physical resource roles. The observed rows include:

| Resource type | Version | Observed role |
| --- | ---: | --- |
| 0 | 0 | original image |
| 0 | 2 | full edited image render |
| 1 | 0 | original standalone video |
| 1 | 2 | full edited standalone video render |
| 3 | 0 | original Live Photo companion MOV |
| 3 | 2 | adjusted companion MOV render |
| 0 | 3 | derivative, not mirrored |
| 14 | 3 | cache/analysis resource, not mirrored |

The physical file size matches `ZINTERNALRESOURCE.ZDATALENGTH` in the inspected examples. Full edited image renders use `resources/renders/<hex>/<UUID>_1_201_a.<extension>`; edited standalone videos use `<UUID>_2_0_a.mov`. Original Live Photo videos use `originals/<hex>/<UUID>_3.mov`; adjusted motion renders use `resources/renders/<hex>/<UUID>_2_100_a.mov`. The companion belongs to the same `ZASSET.Z_PK` as its image, so it is not a standalone video asset. For an edited Live Photo, `live/` holds the adjusted motion render and `orig/` holds the original MOV.

The mirror uses `ZORIGINALFILENAME` for destination names, even when the physical resource has a different extension or letter case. For edited assets, the original goes under `orig/`, while the selected full render goes in the capture-date directory.

No RAW files were present in this sample. The reader can include other full-size original resources when they match a version 0 resource row by size, but RAW+JPEG combinations have not been validated against a real library here. The private Photos schema can change; the reader rejects missing required tables and should be extended only after inspecting a new schema.

## Source read consistency and ownership

Ordinary SQLite `mode=ro` updated the supplied library's `Photos.sqlite-shm` during a read-only test. SQLite `immutable=1` left SHM untouched, but a regression test showed that the Go driver missed tables present only in an uncheckpointed WAL. The program therefore makes a deliberate temporary snapshot of `Photos.sqlite` and its WAL using read-only file handles, checks that both source files stayed stable while copied, and opens the temporary database read-only. SQLite may create a SHM file in that temporary directory. The source database, WAL, and SHM are never opened for writing, checkpointed, or altered. **Close Photos.app before synchronizing**; concurrent edits during or after the snapshot cannot be made fully safe by the stability check, and media files are not part of the SQLite snapshot.

A destination file is removable only if its path is in the prior catalog and its current device and inode still match the catalog record, or if it is a known hard link to a current source resource being moved. Files without that evidence are retained. If a Photos asset still exists but its source file is temporarily unavailable, its previous destination link is retained and the catalog is not replaced until the missing resource is resolved. If the catalog is deleted after a source asset has also disappeared, the old destination link cannot be attributed with confidence and is retained. A missing or corrupt catalog can still be rebuilt for resources that remain in the source.
