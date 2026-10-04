# mac-photos-sync

`mac-photos-sync` creates a date-organized (`YYYY/YYYY-MM/YYYY-MM-DD/*`) hard-link mirror of a local Apple Photos library. It reads the source library and keeps a rebuildable `photos.sqlite` catalog in the destination.

## File over app

Readable files matter more than the closed app that manages them. `mac-photos-sync` follows this philosophy by making photos and videos accessible in dated folders you can browse without Apple Photos.

## Build and run

```sh
make build
make install
./mac-photos-sync -library "/path/to/Photos Library.photoslibrary" -dry-run /path/to/mirror
./mac-photos-sync -library "/path/to/Photos Library.photoslibrary" /path/to/mirror
```

The default library is `~/Pictures/Photos Library.photoslibrary`. Options are `-library`, `-dry-run`, `-verbose`, `-rebuild-db`, `-workers`, and `-help`. `make build-amd64` produces `mac-photos-sync-amd64`; `make install` uses `go install .` to install `mac-photos-sync`.

Media goes under `YYYY/YYYY-MM/YYYY-MM-DD/`. Edited originals go in `orig/`, and Live Photo companion videos go in `live/`. Destination files are hard links, so source and destination must be on the same filesystem. Media is never copied. A second run verifies existing link identity and changes only what differs. `-dry-run` writes neither source nor destination.

Close Photos.app before running a sync so the temporary, read-only database snapshot and media files are coherent. Unavailable iCloud resources are reported and skipped. The tool does not request downloads. A conflict or missing local resource causes a nonzero exit status. Unknown destination files are kept.

The supplied library's schema and resource mapping are described in [docs/photos-library.md](docs/photos-library.md). Photos uses a private schema; this implementation accepts the supplied schema version 5001 and rejects other reported versions. RAW pairs remain unvalidated because the supplied library has no RAW samples.
