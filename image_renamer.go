package main

import (
	"fmt"
	"image"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"
)

var (
	// Matches files already in the new format with numeric capture groups
	vrchatNewFileRe = regexp.MustCompile(`^VRChat_(\d{4})-(\d{2})-(\d{2})_(\d{2})-(\d{2})-(\d{2})\.(\d{3})_(\d+)x(\d+)\.png$`)
	// Matches old-format files: VRChat_<width>x<height>_<date>_<time>.png
	vrchatOldFileRe = regexp.MustCompile(`^VRChat_(\d+)x(\d+)_(\d{4})-(\d{2})-(\d{2})_(\d{2})-(\d{2})-(\d{2})\.(\d{3})\.png$`)
	// Matches directories named YYYY-MM-DD
	dateDirRe = regexp.MustCompile(`^(\d{4})-(\d{2})-(\d{2})$`)
	// Matches files named HH-MM-SS.SSS.png
	timeFileRe = regexp.MustCompile(`^(\d{2})-(\d{2})-(\d{2})\.(\d{3})\.png$`)
)

func main() {
	inputDir := "."
	outputDir := "output"

	err := filepath.Walk(inputDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if info.Name() == outputDir {
				return filepath.SkipDir
			}
			return nil
		}

		// Try new-format first, then old-format, then date-dir files
		if handled, err := processNewFormatFile(path, outputDir); err != nil || handled {
			return err
		}
		if handled, err := processOldFormatFile(path, outputDir); err != nil || handled {
			return err
		}
		if handled, err := processDateDirFile(path, outputDir); err != nil || handled {
			return err
		}
		return nil
	})

	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

type VRChatDate struct {
	year        uint16
	month       uint8
	day         uint8
	hour        uint8
	minute      uint8
	second      uint8
	millisecond uint16
	Timestamp   time.Time
}

// parseParts converts string components of a date/time into a VRChatDate,
// validating that each unit is within standard limits and that the full
// date/time actually exists.
func parseParts(
	year, month, day,
	hour, minute, second,
	millisecond string,
) (VRChatDate, error) {
	// Parse each component
	y64, err := strconv.ParseUint(year, 10, 16)
	if err != nil {
		return VRChatDate{}, fmt.Errorf("invalid year %q: %v", year, err)
	}
	m64, err := strconv.ParseUint(month, 10, 8)
	if err != nil {
		return VRChatDate{}, fmt.Errorf("invalid month %q: %v", month, err)
	}
	d64, err := strconv.ParseUint(day, 10, 8)
	if err != nil {
		return VRChatDate{}, fmt.Errorf("invalid day %q: %v", day, err)
	}
	h64, err := strconv.ParseUint(hour, 10, 8)
	if err != nil {
		return VRChatDate{}, fmt.Errorf("invalid hour %q: %v", hour, err)
	}
	min64, err := strconv.ParseUint(minute, 10, 8)
	if err != nil {
		return VRChatDate{}, fmt.Errorf("invalid minute %q: %v", minute, err)
	}
	s64, err := strconv.ParseUint(second, 10, 8)
	if err != nil {
		return VRChatDate{}, fmt.Errorf("invalid second %q: %v", second, err)
	}
	ms64, err := strconv.ParseUint(millisecond, 10, 16)
	if err != nil {
		return VRChatDate{}, fmt.Errorf("invalid millisecond %q: %v", millisecond, err)
	}

	// Check ranges for each unit
	if y64 < 2000 || y64 > 2500 {
		return VRChatDate{}, fmt.Errorf("year must be 2000-2500, got %d", y64)
	}
	if m64 < 1 || m64 > 12 {
		return VRChatDate{}, fmt.Errorf("month must be 1-12, got %d", m64)
	}
	if d64 < 1 || d64 > 32 {
		return VRChatDate{}, fmt.Errorf("day must be 1-32, got %d", d64)
	}
	if h64 < 0 || h64 > 23 {
		return VRChatDate{}, fmt.Errorf("hour must be 0-23, got %d", h64)
	}
	if min64 < 0 || min64 > 59 {
		return VRChatDate{}, fmt.Errorf("minute must be 0-59, got %d", min64)
	}
	if s64 < 0 || s64 > 59 {
		return VRChatDate{}, fmt.Errorf("second must be 0-59, got %d", s64)
	}
	if ms64 < 0 || ms64 > 999 {
		return VRChatDate{}, fmt.Errorf("millisecond must be 0-999, got %d", ms64)
	}

	// Convert to int for range checking and time.Date
	y := int(y64)
	m := int(m64)
	d := int(d64)
	h := int(h64)
	mi := int(min64)
	s := int(s64)
	ms := int(ms64)

	// Round-trip through time.Date to catch any other anomalies
	t := time.Date(y, time.Month(m), d, h, mi, s, ms*1e6, time.Local)
	if t.Year() != y || int(t.Month()) != m || t.Day() != d ||
		t.Hour() != h || t.Minute() != mi || t.Second() != s ||
		t.Nanosecond() != ms*1e6 {
		return VRChatDate{}, fmt.Errorf(
			"invalid date/time: %04d-%02d-%02d %02d:%02d:%02d.%03d", y, m, d, h, mi, s, ms,
		)
	}

	// All good: return the populated struct
	return VRChatDate{
		year:        uint16(y64),
		month:       uint8(m64),
		day:         uint8(d64),
		hour:        uint8(h64),
		minute:      uint8(min64),
		second:      uint8(s64),
		millisecond: uint16(ms64),
		Timestamp:   t,
	}, nil
}

func processNewFormatFile(path, outputDir string) (bool, error) {
	base := filepath.Base(path)
	m := vrchatNewFileRe.FindStringSubmatch(base)
	if m == nil {
		return false, nil
	}
	dateParts, err := parseParts(m[1], m[2], m[3], m[4], m[5], m[6], m[7])
	if err != nil {
		return true, err
	}
	width, _ := strconv.Atoi(m[8])
	height, _ := strconv.Atoi(m[9])
	return true, copyAndRename(path, dateParts, width, height, outputDir)
}

func processOldFormatFile(path, outputDir string) (bool, error) {
	base := filepath.Base(path)
	m := vrchatOldFileRe.FindStringSubmatch(base)
	if m == nil {
		return false, nil
	}
	width, _ := strconv.Atoi(m[1])
	height, _ := strconv.Atoi(m[2])
	dateParts, err := parseParts(m[3], m[4], m[5], m[6], m[7], m[8], m[9])
	if err != nil {
		return true, err
	}
	return true, copyAndRename(path, dateParts, width, height, outputDir)
}

func processDateDirFile(path, outputDir string) (bool, error) {
	dir := filepath.Base(filepath.Dir(path))
	dm := dateDirRe.FindStringSubmatch(dir)
	if dm == nil {
		return false, nil
	}
	file := filepath.Base(path)
	tm := timeFileRe.FindStringSubmatch(file)
	if tm == nil {
		return false, nil
	}
	dateParts, err := parseParts(dm[1], dm[2], dm[3], tm[1], tm[2], tm[3], tm[4])
	if err != nil {
		return true, err
	}
	width, height, err := getResolution(path)
	if err != nil {
		return false, err
	}
	return true, copyAndRename(path, dateParts, width, height, outputDir)
}

func getResolution(path string) (int, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return 0, 0, err
	}
	return cfg.Width, cfg.Height, nil
}

// copyAndRename copies & renames using VRChatDate, then sets modification time
func copyAndRename(src string, d VRChatDate, width, height int, outputDir string) error {
	ym := fmt.Sprintf("%04d-%02d", d.year, d.month)
	destDir := filepath.Join(outputDir, ym)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return err
	}
	newName := fmt.Sprintf(
		"VRChat_%04d-%02d-%02d_%02d-%02d-%02d.%03d_%dx%d.png",
		d.year, d.month, d.day, d.hour, d.minute, d.second, d.millisecond, width, height,
	)
	destPath := filepath.Join(destDir, newName)

	fmt.Printf("Processing %s -> %s\n", src, destPath)
	if err := copyFile(src, destPath); err != nil {
		return err
	}
	return os.Chtimes(destPath, d.Timestamp, d.Timestamp)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
