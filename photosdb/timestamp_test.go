package photosdb

import (
	"testing"
	"time"
)

func TestAppleTime(test *testing.T) {
	if got := AppleTime(0); !got.Equal(time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)) {
		test.Fatal(got)
	}
	if got := AppleTime(768560002.826); got.Year() != 2025 || got.Month() != time.May || got.Day() != 10 {
		test.Fatal(got)
	}
}
