package get

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"

	"github.com/cheggaaa/pb/v3"
	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"
)

type assignTasksConfig struct {
	Procs         int
	ContentLength int64 // full download filesize
	URLs          []string
	PartialDir    string
	Filename      string
	Client        *http.Client
}

type task struct {
	ID         int
	Procs      int
	URL        string
	Range      Range
	PartialDir string
	Filename   string
	Client     *http.Client
}

func (t *task) destPath() string {
	// Chunk file is named by the task's start offset so a resume with any
	// -p value can map existing bytes back to file positions.
	return getPartialFilePath(t.PartialDir, t.Filename, t.Range.low)
}

func (t *task) String() string {
	return fmt.Sprintf("task[%d]: %q", t.ID, t.destPath())
}

type makeRequestOption struct {
	useragent string
	referer   string
}

func (t *task) makeRequest(ctx context.Context, opt *makeRequestOption) (*http.Request, error) {
	req, err := http.NewRequest("GET", t.URL, nil)
	if err != nil {
		return nil, errors.Wrap(err, fmt.Sprintf("failed to make a new request: %d", t.ID))
	}
	req = req.WithContext(ctx)

	// set download ranges
	req.Header.Set("Range", t.Range.BytesRange())

	// set useragent
	req.Header.Set("User-Agent", opt.useragent)

	// set referer
	if opt.referer != "" {
		req.Header.Set("Referer", opt.referer)
	}

	return req, nil
}

// assignTasks creates tasks for the missing byte ranges only.
// Each task writes to its own offset based chunk file, so resuming with a
// different Procs value reuses already downloaded bytes instead of
// re-downloading or creating another partial folder.
func assignTasks(c *assignTasksConfig) []*task {
	if c.Procs <= 0 {
		c.Procs = 1
	}
	if len(c.URLs) == 0 {
		return nil
	}
	chunks, _ := scanDownloadedChunks(c.PartialDir, c.Filename)
	missing := computeMissingRanges(chunks, c.ContentLength)
	if len(missing) == 0 {
		return nil
	}
	split := splitRangesForProcs(missing, c.Procs)
	tasks := make([]*task, 0, len(split))
	for i, r := range split {
		tasks = append(tasks, &task{
			ID:         i,
			Procs:      c.Procs,
			URL:        c.URLs[i%len(c.URLs)],
			Range:      r,
			PartialDir: c.PartialDir,
			Filename:   c.Filename,
			Client:     c.Client,
		})
	}
	return tasks
}

// splitRangesForProcs splits missing ranges into at most procs tasks,
// splitting the largest ranges first so every connection gets work.
func splitRangesForProcs(missing []Range, procs int) []Range {
	if procs <= 1 {
		out := make([]Range, 0, len(missing))
		for _, r := range missing {
			out = append(out, r)
		}
		return out
	}
	out := make([]Range, 0, len(missing))
	for _, r := range missing {
		out = append(out, r)
	}
	for len(out) < procs {
		// Find the largest splittable range.
		best := -1
		var bestSize int64
		for i, r := range out {
			if sz := rangeSize(r); sz >= 2 && sz > bestSize {
				best, bestSize = i, sz
			}
		}
		if best < 0 {
			break
		}
		r := out[best]
		mid := r.low + rangeSize(r)/2
		first := Range{low: r.low, high: mid - 1}
		second := Range{low: mid, high: r.high}
		out[best] = first
		// Insert the second half right after best without builtin copy
		// (package defines func copy which shadows the builtin).
		var expanded []Range
		expanded = append(expanded, out[:best+1]...)
		expanded = append(expanded, second)
		expanded = append(expanded, out[best+1:]...)
		out = expanded
	}
	return out
}

type DownloadConfig struct {
	Filename      string
	Dirname       string
	ContentLength int64
	Procs         int
	URLs          []string
	Client        *http.Client

	*makeRequestOption
}

type DownloadOption func(c *DownloadConfig)

func WithUserAgent(ua, version string) DownloadOption {
	return func(c *DownloadConfig) {
		if ua == "" {
			ua = "get/" + version
		}
		c.makeRequestOption.useragent = ua
	}
}

func WithReferer(referer string) DownloadOption {
	return func(c *DownloadConfig) {
		c.makeRequestOption.referer = referer
	}
}

func Download(ctx context.Context, c *DownloadConfig, opts ...DownloadOption) error {
	if c.Procs <= 0 {
		c.Procs = 1
	}
	partialDir := getPartialDirname(c.Dirname, c.Filename)

	// create download location
	if err := os.MkdirAll(partialDir, 0755); err != nil {
		return errors.Wrap(err, "failed to mkdir for download location")
	}

	// Adopt data from older layouts (per-procs dirs, index named files)
	// into the single offset based partial dir.
	migrateLegacyPartials(partialDir, c.Dirname, c.Filename, c.ContentLength)

	c.makeRequestOption = &makeRequestOption{}

	for _, opt := range opts {
		opt(c)
	}

	tasks := assignTasks(&assignTasksConfig{
		Procs:         c.Procs,
		ContentLength: c.ContentLength,
		URLs:          c.URLs,
		PartialDir:    partialDir,
		Filename:      c.Filename,
		Client:        newClient(c.Client),
	})

	if err := parallelDownload(ctx, &parallelDownloadConfig{
		ContentLength:     c.ContentLength,
		Tasks:             tasks,
		PartialDir:        partialDir,
		makeRequestOption: c.makeRequestOption,
	}); err != nil {
		return err
	}

	return bindFiles(c, partialDir)
}

type parallelDownloadConfig struct {
	ContentLength int64
	Tasks         []*task
	PartialDir    string
	*makeRequestOption
}

func parallelDownload(ctx context.Context, c *parallelDownloadConfig) error {
	eg, ctx := errgroup.WithContext(ctx)

	bar := pb.Start64(c.ContentLength).SetWriter(stdout).Set(pb.Bytes, true)
	defer bar.Finish()

	// check file size already downloaded for resume
	size, err := checkProgress(c.PartialDir)
	if err != nil {
		return errors.Wrap(err, "failed to get directory size")
	}

	bar.SetCurrent(size)

	for _, task := range c.Tasks {
		eg.Go(func() error {
			req, err := task.makeRequest(ctx, c.makeRequestOption)
			if err != nil {
				return err
			}
			return task.download(req, bar)
		})
	}

	return eg.Wait()
}

func (t *task) download(req *http.Request, bar *pb.ProgressBar) error {
	resp, err := t.Client.Do(req)
	if err != nil {
		return errors.Wrapf(err, "failed to get response: %q", t.String())
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusPartialContent && resp.StatusCode != http.StatusOK {
		return errors.Errorf("unexpected status %q for %q (want 206 Partial Content)", resp.Status, t.String())
	}

	// O_TRUNC (not APPEND): the chunk file belongs to exactly this byte
	// range, so a retried task must overwrite rather than append.
	output, err := os.OpenFile(t.destPath(), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0666)
	if err != nil {
		return errors.Wrapf(err, "failed to create: %q", t.String())
	}
	defer output.Close()

	var src io.Reader = resp.Body
	if resp.StatusCode == http.StatusOK {
		// Server ignored the Range header and sent the whole file.
		// Keep only the slice belonging to this task.
		if _, err := io.CopyN(io.Discard, resp.Body, t.Range.low); err != nil {
			return errors.Wrapf(err, "failed to skip to range start: %q", t.String())
		}
		src = io.LimitReader(resp.Body, rangeSize(t.Range))
	}
	rd := bar.NewProxyReader(src)

	if _, err := io.Copy(output, rd); err != nil {
		return errors.Wrapf(err, "failed to write response body: %q", t.String())
	}

	return nil
}

func bindFiles(c *DownloadConfig, partialDir string) error {
	fmt.Fprintln(stdout, "\nbinding with files...")

	chunks, err := scanDownloadedChunks(partialDir, c.Filename)
	if err != nil {
		return errors.Wrap(err, "failed to scan partial files")
	}
	// Validate full coverage before joining; otherwise a short chunk would
	// silently produce a corrupt output.
	if missing := computeMissingRanges(chunks, c.ContentLength); len(missing) > 0 {
		return errors.Errorf("missing %d byte range(s), download incomplete", len(missing))
	}
	// Order chunks by start offset.
	sort.Slice(chunks, func(i, j int) bool { return chunks[i].offset < chunks[j].offset })

	destPath := filepath.Join(c.Dirname, c.Filename)
	f, err := os.Create(destPath)
	if err != nil {
		return errors.Wrap(err, "failed to create a file in download location")
	}
	defer f.Close()

	bar := pb.Start64(c.ContentLength).SetWriter(stdout)

	copyFn := func(name string) error {
		subfp, err := os.Open(name)
		if err != nil {
			return errors.Wrapf(err, "failed to open %q in download location", name)
		}

		defer subfp.Close()

		proxy := bar.NewProxyReader(subfp)
		if _, err := io.Copy(f, proxy); err != nil {
			return errors.Wrapf(err, "failed to copy %q", name)
		}

		return nil
	}

	for _, chunk := range chunks {
		partialFilename := getPartialFilePath(partialDir, c.Filename, chunk.offset)
		if err := copyFn(partialFilename); err != nil {
			return err
		}

		// remove a file in download location for join
		if err := os.Remove(partialFilename); err != nil {
			return errors.Wrapf(err, "failed to remove %q in download location", partialFilename)
		}
	}

	bar.Finish()

	// remove download location
	// RemoveAll reason: will create .DS_Store in download location if execute on mac
	if err := os.RemoveAll(partialDir); err != nil {
		return errors.Wrap(err, "failed to remove download location")
	}

	return nil
}
