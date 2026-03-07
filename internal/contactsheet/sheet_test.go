package contactsheet

import (
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
	"time"

	"sauron-sees/internal/metadata"
)

func TestBuildLimitsSheetsPerDay(t *testing.T) {
	tempDir := t.TempDir()
	var records []metadata.CaptureRecord
	for hour := 0; hour < 12; hour++ {
		for idx := 0; idx < 2; idx++ {
			imagePath := filepath.Join(tempDir, fmt.Sprintf("img-%02d-%02d.jpg", hour, idx))
			writeTestJPEG(t, imagePath, color.RGBA{uint8(hour * 20), uint8(idx * 40), 90, 255})
			ts := time.Date(2026, 3, 9, hour, idx*10, 0, 0, time.UTC)
			records = append(records, metadata.CaptureRecord{
				Timestamp: ts.Format(time.RFC3339),
				ImagePath: imagePath,
			})
		}
	}

	sheets, err := Build("2026-03-09", filepath.Join(tempDir, "sheets"), records)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if got := len(sheets); got != 10 {
		t.Fatalf("len(sheets) = %d, want 10", got)
	}
}

func writeTestJPEG(t *testing.T, path string, c color.RGBA) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1280, 720))
	for y := 0; y < 720; y++ {
		for x := 0; x < 1280; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create test image: %v", err)
	}
	defer f.Close()
	if err := jpeg.Encode(f, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatalf("encode test image: %v", err)
	}
}
