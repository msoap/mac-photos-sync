package model

import "time"

type Metadata struct {
	Extracted   bool           `json:"extracted,omitempty"`
	CaptureTime *time.Time     `json:"capture_time,omitempty"`
	Width       int            `json:"width,omitempty"`
	Height      int            `json:"height,omitempty"`
	DurationMS  int64          `json:"duration_ms,omitempty"`
	Make        string         `json:"make,omitempty"`
	Model       string         `json:"model,omitempty"`
	LensModel   string         `json:"lens_model,omitempty"`
	Latitude    *float64       `json:"latitude,omitempty"`
	Longitude   *float64       `json:"longitude,omitempty"`
	Orientation int            `json:"orientation,omitempty"`
	MIMEType    string         `json:"mime_type,omitempty"`
	Format      string         `json:"format,omitempty"`
	Raw         map[string]any `json:"raw,omitempty"`
}

type Asset struct {
	UUID                string
	OriginalFilename    string
	FilenameSource      string
	CaptureTime         time.Time
	CaptureTimeSource   string
	TimezoneOffset      int
	MediaType           string
	Width, Height       int
	DurationMS          int64
	Latitude, Longitude *float64
	Favorite, Hidden    bool
	Metadata            Metadata
	Resources           []Resource
}

type Resource struct {
	AssetUUID           string
	Type                string
	SourcePath          string
	DestinationPath     string
	OriginalFilename    string
	DestinationFilename string
	Size                int64
	Device, Inode       uint64
	Metadata            Metadata
	Missing             bool
}
