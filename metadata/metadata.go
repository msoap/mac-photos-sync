package metadata

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/barasher/go-exiftool"
	"github.com/bep/imagemeta"
	"github.com/msoap/mac-photos-sync/model"
)

type MetadataReader interface {
	Read(path string) (*model.Metadata, error)
}

type Reader struct {
	mu   sync.Mutex
	exif *exiftool.Exiftool
}

func NewReader() *Reader { return &Reader{} }

func (reader *Reader) Close() error {
	reader.mu.Lock()
	defer reader.mu.Unlock()
	if reader.exif != nil {
		return reader.exif.Close()
	}
	return nil
}

func (reader *Reader) Read(path string) (*model.Metadata, error) {
	if m, err := readImage(path); err == nil {
		return m, nil
	}
	reader.mu.Lock()
	defer reader.mu.Unlock()
	if reader.exif == nil {
		et, err := exiftool.NewExiftool(exiftool.NoPrintConversion())
		if err != nil {
			return &model.Metadata{}, err
		}
		reader.exif = et
	}
	fm := reader.exif.ExtractMetadata(path)
	if len(fm) == 0 {
		return &model.Metadata{}, fmt.Errorf("ExifTool returned no metadata: %s", path)
	}
	if fm[0].Err != nil {
		return &model.Metadata{}, fm[0].Err
	}
	metadata := &model.Metadata{Raw: fm[0].Fields}
	metadata.Make = str(metadata.Raw, "Make")
	metadata.Model = str(metadata.Raw, "Model")
	metadata.LensModel = str(metadata.Raw, "LensModel")
	metadata.Format = str(metadata.Raw, "FileType")
	metadata.MIMEType = str(metadata.Raw, "MIMEType")
	metadata.Width = num(metadata.Raw, "ImageWidth")
	metadata.Height = num(metadata.Raw, "ImageHeight")
	metadata.Orientation = num(metadata.Raw, "Orientation")
	if v, err := fm[0].GetFloat("Duration"); err == nil {
		metadata.DurationMS = int64(v * 1000)
	}
	if v, err := fm[0].GetFloat("GPSLatitude"); err == nil {
		metadata.Latitude = &v
	}
	if v, err := fm[0].GetFloat("GPSLongitude"); err == nil {
		metadata.Longitude = &v
	}
	for _, k := range []string{"DateTimeOriginal", "MediaCreateDate", "CreateDate", "TrackCreateDate"} {
		if t := parseTime(str(metadata.Raw, k)); !t.IsZero() {
			metadata.CaptureTime = &t
			break
		}
	}
	return metadata, nil
}

func readImage(path string) (*model.Metadata, error) {
	formats := map[string]imagemeta.ImageFormat{".jpg": imagemeta.JPEG, ".jpeg": imagemeta.JPEG, ".tif": imagemeta.TIFF, ".tiff": imagemeta.TIFF, ".png": imagemeta.PNG, ".webp": imagemeta.WebP, ".heic": imagemeta.HEIF, ".heif": imagemeta.HEIF, ".avif": imagemeta.AVIF, ".dng": imagemeta.DNG, ".cr2": imagemeta.CR2, ".nef": imagemeta.NEF, ".arw": imagemeta.ARW, ".pef": imagemeta.PEF}
	ext := strings.ToLower(filepath.Ext(path))
	format, ok := formats[ext]
	if !ok {
		return nil, fmt.Errorf("unsupported image format: %s", ext)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	metadata := &model.Metadata{Raw: make(map[string]any), Format: strings.TrimPrefix(strings.ToUpper(ext), "."), MIMEType: mime(ext)}
	res, err := imagemeta.Decode(imagemeta.Options{R: file, ImageFormat: format, Sources: imagemeta.EXIF | imagemeta.IPTC | imagemeta.XMP | imagemeta.CONFIG, HandleTag: func(tag imagemeta.TagInfo) error {
		key := fmt.Sprintf("%d:%s:%s", tag.Source, tag.Namespace, tag.Tag)
		metadata.Raw[key] = tag.Value
		switch tag.Tag {
		case "DateTimeOriginal":
			if x := parseTime(fmt.Sprint(tag.Value)); !x.IsZero() {
				metadata.CaptureTime = &x
			}
		case "Make":
			metadata.Make = fmt.Sprint(tag.Value)
		case "Model":
			metadata.Model = fmt.Sprint(tag.Value)
		case "LensModel":
			metadata.LensModel = fmt.Sprint(tag.Value)
		case "Orientation":
			fmt.Sscan(fmt.Sprint(tag.Value), &metadata.Orientation)
		}
		return nil
	}})
	if err != nil {
		return nil, err
	}
	metadata.Width = res.ImageConfig.Width
	metadata.Height = res.ImageConfig.Height
	return metadata, nil
}

func mime(ext string) string {
	switch ext {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".heic", ".heif":
		return "image/heic"
	case ".png":
		return "image/png"
	case ".tif", ".tiff":
		return "image/tiff"
	}
	return ""
}

func str(tags map[string]any, key string) string {
	value, ok := tags[key]
	if !ok {
		return ""
	}
	return fmt.Sprint(value)
}

func num(tags map[string]any, key string) int { var n int; fmt.Sscan(str(tags, key), &n); return n }

func parseTime(timestamp string) time.Time {
	for _, layout := range []string{"2006:01:02 15:04:05-07:00", "2006:01:02 15:04:05Z07:00", "2006:01:02 15:04:05", "2006-01-02T15:04:05Z07:00", "2006-01-02 15:04:05-07:00", "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, timestamp); err == nil {
			return t
		}
	}
	return time.Time{}
}
