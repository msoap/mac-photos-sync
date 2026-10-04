package sync

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/msoap/mac-photos-sync/catalog"
	"github.com/msoap/mac-photos-sync/filesystem"
	"github.com/msoap/mac-photos-sync/metadata"
	"github.com/msoap/mac-photos-sync/model"
	"github.com/msoap/mac-photos-sync/photosdb"
)

type Options struct {
	Library, Destination       string
	DryRun, Verbose, RebuildDB bool
	Workers                    int
	Output                     io.Writer
}

type Summary struct {
	Assets, Resources, Existing, Created, Moved, Removed, Repaired, Conflicts, Missing, MetadataUpdated int
}

func Run(ctx context.Context, options Options) (Summary, error) {
	var result Summary
	if options.Output == nil {
		options.Output = os.Stdout
	}
	if options.Workers <= 0 {
		options.Workers = runtime.NumCPU()
		if options.Workers > 8 {
			options.Workers = 8
		}
	}
	root, err := filepath.Abs(options.Destination)
	if err != nil {
		return result, err
	}
	library, err := filepath.Abs(options.Library)
	if err != nil {
		return result, err
	}
	root, err = canonicalPath(root)
	if err != nil {
		return result, err
	}
	library, err = canonicalPath(library)
	if err != nil {
		return result, err
	}
	if root == library || strings.HasPrefix(root, library+string(os.PathSeparator)) {
		return result, errors.New("destination must not be inside the Photos library")
	}
	fmt.Fprintln(options.Output, "Scanning Photos library...")
	libraryReader, err := photosdb.Open(library)
	if err != nil {
		return result, err
	}
	defer libraryReader.Close()
	state, err := libraryReader.Scan(ctx)
	if err != nil {
		return result, err
	}
	result.Assets = len(state.Assets)
	result.Missing = len(state.Missing)
	for _, path := range state.Missing {
		fmt.Fprintln(options.Output, "MISSING", path)
	}
	old, err := catalog.Read(root)
	if err != nil {
		fmt.Fprintf(options.Output, "Catalog unavailable; rebuilding from source and destination: %v\n", err)
		old = catalog.State{Resources: make(map[string]catalog.Record)}
	}
	if options.RebuildDB {
		fmt.Fprintln(options.Output, "Rebuilding destination catalog...")
	}
	result.MetadataUpdated = extractMetadata(ctx, state.Assets, old, options.Workers, options.RebuildDB, options.Output, options.Verbose)
	plan, err := Build(root, state.Assets, old)
	if err != nil {
		return result, err
	}
	result.Existing = plan.Existing
	result.Missing = plan.Missing
	result.Conflicts = len(plan.Conflicts)
	for _, a := range state.Assets {
		for _, r := range a.Resources {
			if !r.Missing {
				result.Resources++
			}
		}
	}
	for _, op := range plan.Operations {
		switch op.Kind {
		case "CREATE_LINK":
			result.Created++
		case "MOVE_LINK":
			result.Moved++
		case "REMOVE_LINK":
			result.Removed++
		case "REPAIR_LINK":
			result.Repaired++
		}
	}
	for _, c := range plan.Conflicts {
		fmt.Fprintln(options.Output, "CONFLICT", c)
	}
	if err = CheckDevice(root, plan); err != nil {
		return result, err
	}
	if options.DryRun {
		for _, op := range plan.Operations {
			fmt.Fprintf(options.Output, "%s %s%s\n", op.Kind, op.From, formatTo(op.To))
		}
		summary(options.Output, result, true)
		if result.Conflicts > 0 || result.Missing > 0 {
			return result, errors.New("synchronization incomplete")
		}
		return result, nil
	}
	if err = os.MkdirAll(root, 0755); err != nil {
		return result, err
	}
	if err = Apply(root, plan, func(op Operation) {
		if options.Verbose {
			fmt.Fprintf(options.Output, "%s %s%s\n", op.Kind, op.From, formatTo(op.To))
		}
	}); err != nil {
		return result, err
	}
	if result.Conflicts == 0 && result.Missing == 0 {
		// Re-stat every linked resource before catalog commit. The database never
		// claims a link that did not reach its final destination.
		for ai := range state.Assets {
			for ri := range state.Assets[ai].Resources {
				resource := &state.Assets[ai].Resources[ri]
				if resource.Missing {
					continue
				}
				src, err := filesystem.Stat(resource.SourcePath)
				if err != nil {
					return result, err
				}
				dst, err := filesystem.Stat(filepath.Join(root, resource.DestinationPath))
				if err != nil {
					return result, err
				}
				if !filesystem.Same(src, dst) {
					return result, fmt.Errorf("link identity changed: %s", resource.DestinationPath)
				}
				resource.Device = src.Device
				resource.Inode = src.Inode
				resource.Size = src.Size
			}
		}
		if err = catalog.Write(root, state.Assets); err != nil {
			return result, fmt.Errorf("catalog commit: %w", err)
		}
	}
	summary(options.Output, result, false)
	if result.Conflicts > 0 || result.Missing > 0 {
		return result, errors.New("synchronization incomplete")
	}
	return result, nil
}

func canonicalPath(path string) (string, error) {
	var missing []string
	candidate := path
	for {
		resolved, err := filepath.EvalSymlinks(candidate)
		if err == nil {
			for i := len(missing) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, missing[i])
			}
			return resolved, nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(candidate)
		if parent == candidate {
			return "", err
		}
		missing = append(missing, filepath.Base(candidate))
		candidate = parent
	}
}

func formatTo(to string) string {
	if to != "" {
		return " -> " + to
	}
	return ""
}

func summary(output io.Writer, result Summary, dry bool) {
	mode := ""
	if dry {
		mode = " (dry run)"
	}
	fmt.Fprintf(output, "Assets: %d  Resources: %d  Existing: %d  New: %d  Moved: %d  Removed: %d  Repaired: %d  Conflicts: %d  Missing: %d  Metadata updated: %d%s\n", result.Assets, result.Resources, result.Existing, result.Created, result.Moved, result.Removed, result.Repaired, result.Conflicts, result.Missing, result.MetadataUpdated, mode)
}

func extractMetadata(ctx context.Context, assets []model.Asset, old catalog.State, workers int, rebuild bool, out io.Writer, verbose bool) int {
	cache := make(map[string]catalog.Record)
	if !rebuild {
		for _, record := range old.Resources {
			cache[record.SourcePath] = record
		}
	}
	type job struct{ assetIndex, resourceIndex int }
	jobs := make(chan job)
	var wg sync.WaitGroup
	var mu sync.Mutex
	updated := 0
	for workerIndex := 0; workerIndex < workers; workerIndex++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			reader := metadata.NewReader()
			defer reader.Close()
			for work := range jobs {
				if ctx.Err() != nil {
					continue
				}
				resource := &assets[work.assetIndex].Resources[work.resourceIndex]
				if resource.Missing {
					continue
				}
				id, err := filesystem.Stat(resource.SourcePath)
				if err != nil {
					continue
				}
				if cached, ok := cache[resource.SourcePath]; ok && cached.Device == id.Device && cached.Inode == id.Inode && cached.Size == id.Size && (cached.Metadata.Extracted || cached.Metadata.Raw != nil) {
					resource.Metadata = cached.Metadata
					continue
				}
				details, err := reader.Read(resource.SourcePath)
				if err != nil {
					if verbose {
						mu.Lock()
						fmt.Fprintf(out, "META warning %s: %v\n", resource.SourcePath, err)
						mu.Unlock()
					}
					continue
				}
				if details.Raw == nil {
					details.Raw = map[string]any{}
				}
				details.Extracted = true
				resource.Metadata = *details
				mu.Lock()
				updated++
				mu.Unlock()
			}
		}()
	}
	for assetIndex := range assets {
		for resourceIndex := range assets[assetIndex].Resources {
			jobs <- job{assetIndex, resourceIndex}
		}
	}
	close(jobs)
	wg.Wait()
	for ai := range assets {
		asset := &assets[ai]
		sort.SliceStable(asset.Resources, func(i, j int) bool {
			return asset.Resources[i].Type == "adjusted" && asset.Resources[j].Type != "adjusted"
		})
		for _, resource := range asset.Resources {
			if resource.Missing {
				continue
			}
			details := resource.Metadata
			if asset.CaptureTime.IsZero() && details.CaptureTime != nil {
				asset.CaptureTime = *details.CaptureTime
				asset.CaptureTimeSource = "metadata"
			}
			if asset.FilenameSource == "filesystem" && details.Raw != nil {
				for _, k := range []string{"OriginalFileName", "OriginalFilename"} {
					if v, ok := details.Raw[k]; ok {
						name := sanitize(fmt.Sprint(v))
						if name != "unnamed" {
							asset.OriginalFilename = name
							asset.FilenameSource = "metadata"
						}
						break
					}
				}
			}
			if asset.Metadata.Raw == nil {
				asset.Metadata = details
			}
			if asset.Width == 0 {
				asset.Width = details.Width
			}
			if asset.Height == 0 {
				asset.Height = details.Height
			}
			if asset.DurationMS == 0 {
				asset.DurationMS = details.DurationMS
			}
			if asset.Latitude == nil {
				asset.Latitude = details.Latitude
			}
			if asset.Longitude == nil {
				asset.Longitude = details.Longitude
			}
		}
		if asset.FilenameSource == "metadata" {
			for ri := range asset.Resources {
				resource := &asset.Resources[ri]
				resource.OriginalFilename = strings.TrimSuffix(asset.OriginalFilename, filepath.Ext(asset.OriginalFilename)) + filepath.Ext(resource.OriginalFilename)
			}
		}
	}
	return updated
}
