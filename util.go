package get

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// getPartialDirname returns the single partial directory for a download.
// It intentionally does NOT contain the procs count so that resuming with
// a different -p value reuses the same folder instead of creating a new one.
func getPartialDirname(targetDir, filename string) string {
	if targetDir == "" {
		return fmt.Sprintf("_%s.partial", filename)
	}
	return filepath.Join(targetDir, fmt.Sprintf("_%s.partial", filename))
}

// chunkFilePrefix is the infix used for offset based partial files.
// It is intentionally distinct from legacy names (e.g. "file.name.3.0"
// or "file.name.0") so new chunks can be told apart from legacy ones.
const chunkFilePrefix = ".part-"

// getPartialFilePath returns the path of the partial file that holds the
// bytes starting at the given absolute file offset.
// Naming files by start offset (instead of task index) makes resume
// independent from the -p (procs) value used in previous runs.
func getPartialFilePath(targetDir, filename string, offset int64) string {
	return filepath.Join(
		targetDir,
		fmt.Sprintf("%s%s%d", filename, chunkFilePrefix, offset),
	)
}

// checkProgress sums only real chunk data so progress bar and resume math
// stay correct even with stray files (.DS_Store, temp files) in the folder.
func checkProgress(dirname string) (int64, error) {
	return chunkDirSize(dirname)
}

func chunkDirSize(dirname string) (int64, error) {
	var size int64
	err := filepath.Walk(dirname, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			// Ignore unreadable entries; stale temp files must not
			// break resume.
			return nil
		}
		if info == nil || info.IsDir() {
			return nil
		}
		name := info.Name()
		if strings.HasSuffix(name, ".migrate-tmp") {
			return nil
		}
		if name == ".DS_Store" {
			return nil
		}
		size += info.Size()
		return nil
	})
	if err != nil {
		return size, err
	}
	return size, nil
}

func (r Range) BytesRange() string {
	return fmt.Sprintf("bytes=%d-%d", r.low, r.high)
}

// rangeSize returns the number of bytes in an inclusive Range.
func rangeSize(r Range) int64 {
	if r.high < r.low {
		return 0
	}
	return r.high - r.low + 1
}

// downloadedChunk is a contiguous block already present on disk.
type downloadedChunk struct {
	offset int64 // absolute start offset in the final file
	size   int64 // bytes already downloaded for this chunk
}

func (c downloadedChunk) end() int64 {
	return c.offset + c.size // exclusive
}

// scanDownloadedChunks lists offset based chunk files in partialDir.
func scanDownloadedChunks(partialDir, filename string) ([]downloadedChunk, error) {
	entries, err := os.ReadDir(partialDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	prefix := filename + chunkFilePrefix
	var chunks []downloadedChunk
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		offStr := strings.TrimPrefix(name, prefix)
		offset, err := strconv.ParseInt(offStr, 10, 64)
		if err != nil || offset < 0 {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.Size() == 0 {
			// Empty placeholder: ignore, it will be re-downloaded.
			continue
		}
		chunks = append(chunks, downloadedChunk{offset: offset, size: info.Size()})
	}
	sort.Slice(chunks, func(i, j int) bool { return chunks[i].offset < chunks[j].offset })
	return chunks, nil
}

// migrateLegacyPartials moves data from pre-offset layouts into the new
// offset based layout inside partialDir. It handles:
//   - old dirs:  _<filename>.<procs>/  with files <filename>.<procs>.<id>
//   - interim files in partialDir: <filename>.<id> (task index, procs unknown)
//
// Migration is best effort: missing data is simply re-downloaded.
// Successfully migrated legacy dirs are removed so only a single
// partial folder remains.
func migrateLegacyPartials(partialDir, parentDir, filename string, contentLength int64) {
	if contentLength <= 0 {
		return
	}
	if parentDir == "" {
		parentDir = "."
	}
	// 1) Old style sibling dirs: _<filename>.<N>
	entries, err := os.ReadDir(parentDir)
	if err == nil {
		legacyPrefix := "_" + filename + "."
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			name := e.Name()
			if name == "_"+filename+".partial" {
				continue
			}
			if !strings.HasPrefix(name, legacyPrefix) {
				continue
			}
			procsStr := strings.TrimPrefix(name, legacyPrefix)
			procs, err := strconv.Atoi(procsStr)
			if err != nil || procs <= 0 {
				continue
			}
			legacyDir := filepath.Join(parentDir, name)
			migrateLegacyDir(legacyDir, partialDir, filename, procs, contentLength)
		}
	}
	// 2) Interim index files inside the new partial dir: <filename>.<id>
	migrateInterimIndexFiles(partialDir, filename, contentLength)
}

func migrateLegacyDir(legacyDir, partialDir, filename string, procs int, contentLength int64) {
	entries, err := os.ReadDir(legacyDir)
	if err != nil {
		return
	}
	taskSize := contentLength / int64(procs)
	prefix := fmt.Sprintf("%s.%d.", filename, procs)
	migratedAny := false
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		id, err := strconv.Atoi(strings.TrimPrefix(name, prefix))
		if err != nil || id < 0 || id >= procs {
			continue
		}
		info, err := e.Info()
		if err != nil || info.Size() == 0 {
			continue
		}
		offset := taskSize * int64(id)
		if offset >= contentLength {
			continue
		}
		if copyLegacyChunk(filepath.Join(legacyDir, name), partialDir, filename, offset) {
			migratedAny = true
		}
	}
	// Remove the old per-procs folder so only one partial dir remains.
	if migratedAny {
		_ = os.RemoveAll(legacyDir)
	}
}

// migrateInterimIndexFiles handles files like <filename>.<id> left by the
// intermediate layout where only the task index was stored. procs is inferred
// as maxID+1 assuming a contiguous 0-based run.
func migrateInterimIndexFiles(partialDir, filename string, contentLength int64) {
	entries, err := os.ReadDir(partialDir)
	if err != nil {
		return
	}
	prefix := filename + "."
	type legacyFile struct {
		name string
		id   int
		size int64
	}
	var files []legacyFile
	maxID := -1
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		rest := strings.TrimPrefix(name, prefix)
		// Skip new offset chunks ("part-<off>") and dotted names.
		if strings.Contains(rest, ".") || strings.Contains(rest, "-") {
			continue
		}
		id, err := strconv.Atoi(rest)
		if err != nil || id < 0 {
			continue
		}
		info, err := e.Info()
		if err != nil || info.Size() == 0 {
			continue
		}
		files = append(files, legacyFile{name: name, id: id, size: info.Size()})
		if id > maxID {
			maxID = id
		}
	}
	if len(files) == 0 {
		return
	}
	procs := maxID + 1
	if procs <= 0 {
		return
	}
	// Guard: a single huge file with a tiny index is likely the corrupted
	// shared file from the buggy layout (all tasks appended to the same
	// path). Discard it instead of trusting the index as offset.
	if len(files) == 1 && files[0].size > contentLength/2 && contentLength > 1<<20 {
		_ = os.Remove(filepath.Join(partialDir, files[0].name))
		return
	}
	taskSize := contentLength / int64(procs)
	for _, f := range files {
		offset := taskSize * int64(f.id)
		if offset >= contentLength {
			continue
		}
		if copyLegacyChunk(filepath.Join(partialDir, f.name), partialDir, filename, offset) {
			_ = os.Remove(filepath.Join(partialDir, f.name))
		}
	}
}

// copyLegacyChunk copies a legacy partial file to its offset based name.
// If the destination already exists the larger file wins.
func copyLegacyChunk(src, partialDir, filename string, offset int64) bool {
	dst := getPartialFilePath(partialDir, filename, offset)
	if src == dst {
		return false
	}
	srcInfo, err := os.Stat(src)
	if err != nil || srcInfo.Size() == 0 {
		return false
	}
	if dstInfo, err := os.Stat(dst); err == nil {
		if dstInfo.Size() >= srcInfo.Size() {
			return true
		}
	}
	if err := os.MkdirAll(partialDir, 0755); err != nil {
		return false
	}
	in, err := os.Open(src)
	if err != nil {
		return false
	}
	defer in.Close()
	tmp := dst + ".migrate-tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return false
	}
	_, copyErr := out.ReadFrom(in)
	closeErr := out.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(tmp)
		return false
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return false
	}
	return true
}

// computeMissingRanges returns the gaps in [0, contentLength) not covered
// by the given downloaded chunks. Overlapping chunks are merged.
func computeMissingRanges(chunks []downloadedChunk, contentLength int64) []Range {
	if contentLength <= 0 {
		return nil
	}
	// Merge overlapping/adjacent chunks and clip to file bounds.
	type interval struct{ low, high int64 } // [low, high) exclusive end
	var merged []interval
	for _, c := range chunks {
		low := c.offset
		high := c.end()
		if high <= 0 || low >= contentLength {
			continue
		}
		if low < 0 {
			low = 0
		}
		if high > contentLength {
			high = contentLength
		}
		if high <= low {
			continue
		}
		if n := len(merged); n > 0 && low <= merged[n-1].high {
			if high > merged[n-1].high {
				merged[n-1].high = high
			}
			continue
		}
		merged = append(merged, interval{low: low, high: high})
	}
	var missing []Range
	prev := int64(0)
	for _, m := range merged {
		if m.low > prev {
			// Range uses inclusive high, so subtract 1.
			missing = append(missing, Range{low: prev, high: m.low - 1})
		}
		if m.high > prev {
			prev = m.high
		}
	}
	if prev < contentLength {
		missing = append(missing, Range{low: prev, high: contentLength - 1})
	}
	return missing
}
