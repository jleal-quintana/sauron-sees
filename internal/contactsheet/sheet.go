package contactsheet

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"sort"
	"time"

	"sauron-sees/internal/metadata"
)

const (
	maxSheetsPerDay  = 10
	maxImagesPerHour = 4
	cellWidth        = 800
	cellHeight       = 450
)

func Build(day string, dir string, records []metadata.CaptureRecord, location *time.Location) ([]string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir sheets dir: %w", err)
	}

	grouped := map[string][]metadata.CaptureRecord{}
	var hours []string
	for _, rec := range records {
		ts := rec.TimeIn(location)
		if ts.IsZero() {
			continue
		}
		key := ts.Format("15")
		if _, ok := grouped[key]; !ok {
			hours = append(hours, key)
		}
		grouped[key] = append(grouped[key], rec)
	}
	sort.Strings(hours)
	hours = sampleHours(hours, maxSheetsPerDay)

	var output []string
	for _, hour := range hours {
		path := filepath.Join(dir, fmt.Sprintf("%s-%s00.jpg", day, hour))
		if err := buildHourSheet(path, sampleRecords(grouped[hour], maxImagesPerHour)); err != nil {
			return nil, err
		}
		output = append(output, path)
	}
	return output, nil
}

func buildHourSheet(dest string, records []metadata.CaptureRecord) error {
	canvas := image.NewRGBA(image.Rect(0, 0, cellWidth*2, cellHeight*2))
	draw.Draw(canvas, canvas.Bounds(), &image.Uniform{C: color.RGBA{20, 20, 20, 255}}, image.Point{}, draw.Src)

	for i, rec := range records {
		f, err := os.Open(rec.ImagePath)
		if err != nil {
			return fmt.Errorf("open source image: %w", err)
		}
		img, _, err := image.Decode(f)
		f.Close()
		if err != nil {
			return fmt.Errorf("decode source image: %w", err)
		}
		scaled := scaleToFit(img, cellWidth, cellHeight)
		col := i % 2
		row := i / 2
		x := col * cellWidth
		y := row * cellHeight
		dstRect := image.Rect(x, y, x+scaled.Bounds().Dx(), y+scaled.Bounds().Dy())
		draw.Draw(canvas, dstRect, scaled, image.Point{}, draw.Over)
	}

	f, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("create sheet: %w", err)
	}
	defer f.Close()
	if err := jpeg.Encode(f, canvas, &jpeg.Options{Quality: 75}); err != nil {
		return fmt.Errorf("encode sheet: %w", err)
	}
	return nil
}

func scaleToFit(src image.Image, maxW int, maxH int) *image.RGBA {
	bounds := src.Bounds()
	srcW := bounds.Dx()
	srcH := bounds.Dy()
	if srcW <= 0 || srcH <= 0 {
		return image.NewRGBA(image.Rect(0, 0, 1, 1))
	}
	scale := minFloat(float64(maxW)/float64(srcW), float64(maxH)/float64(srcH))
	if scale > 1 {
		scale = 1
	}
	dstW := int(float64(srcW) * scale)
	dstH := int(float64(srcH) * scale)
	if dstW < 1 {
		dstW = 1
	}
	if dstH < 1 {
		dstH = 1
	}

	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	for y := 0; y < dstH; y++ {
		for x := 0; x < dstW; x++ {
			srcX := bounds.Min.X + x*srcW/dstW
			srcY := bounds.Min.Y + y*srcH/dstH
			dst.Set(x, y, src.At(srcX, srcY))
		}
	}
	return dst
}

func sampleHours(hours []string, max int) []string {
	if len(hours) <= max {
		return hours
	}
	return sampleStrings(hours, max)
}

func sampleRecords(records []metadata.CaptureRecord, max int) []metadata.CaptureRecord {
	if len(records) <= max {
		return records
	}
	indexes := sampleIndexes(len(records), max)
	out := make([]metadata.CaptureRecord, 0, len(indexes))
	for _, idx := range indexes {
		out = append(out, records[idx])
	}
	return out
}

func sampleStrings(values []string, max int) []string {
	indexes := sampleIndexes(len(values), max)
	out := make([]string, 0, len(indexes))
	for _, idx := range indexes {
		out = append(out, values[idx])
	}
	return out
}

func sampleIndexes(length int, max int) []int {
	if length <= max {
		indexes := make([]int, length)
		for i := range indexes {
			indexes[i] = i
		}
		return indexes
	}
	out := make([]int, 0, max)
	step := float64(length-1) / float64(max-1)
	seen := map[int]bool{}
	for i := 0; i < max; i++ {
		idx := int(float64(i) * step)
		if idx >= length {
			idx = length - 1
		}
		if !seen[idx] {
			seen[idx] = true
			out = append(out, idx)
		}
	}
	for idx := 0; len(out) < max && idx < length; idx++ {
		if !seen[idx] {
			seen[idx] = true
			out = append(out, idx)
		}
	}
	sort.Ints(out)
	return out
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func HourBuckets(records []metadata.CaptureRecord, location *time.Location) map[string]int {
	out := map[string]int{}
	for _, rec := range records {
		ts := rec.TimeIn(location)
		if ts.IsZero() {
			continue
		}
		out[ts.Format("15:00")]++
	}
	return out
}
