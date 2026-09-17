package get

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mholt/archiver"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
)

func TestRunResume(t *testing.T) {
	// listening file server
	mux := http.NewServeMux()

	mux.HandleFunc("/file.name", func(w http.ResponseWriter, r *http.Request) {
		fp := filepath.Join("_testdata", "test.tar.gz")
		data, err := os.ReadFile(fp)
		if err != nil {
			t.Errorf("failed to readfile: %s", err)
		}
		http.ServeContent(w, r, fp, time.Now(), bytes.NewReader(data))
	})

	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	url := ts.URL
	targetURL := fmt.Sprintf("%s/%s", url, "file.name")
	tmpDir := t.TempDir()

	// resume.tar.gz is included resumable file structures.
	// _file.name.3
	// ├── file.name.3.0
	// ├── file.name.3.1
	// └── file.name.3.2
	resumeFilePath := filepath.Join(tmpDir, "resume.tar.gz")
	if err := copy(
		filepath.Join("_testdata", "resume.tar.gz"),
		resumeFilePath,
	); err != nil {
		t.Fatalf("failed to copy: %s", err)
	}

	if err := archiver.NewTarGz().Unarchive(resumeFilePath, tmpDir); err != nil {
		t.Fatalf("failed to untargz: %s", err)
	}

	p := New()
	if err := p.Run(context.Background(), version, []string{
		"get",
		"-p",
		"3",
		targetURL,
		"--timeout",
		"5",
		"--output",
		tmpDir,
	}); err != nil {
		t.Errorf("failed to Run: %s", err)
	}

	cmpFileChecksum(t,
		filepath.Join("_testdata", "test.tar.gz"),
		filepath.Join(tmpDir, "file.name"),
	)
}

func copy(src, dest string) error {
	srcp, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcp.Close()

	dst, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer dst.Close()

	if _, err = io.Copy(dst, srcp); err != nil {
		return err
	}

	return nil
}

const version = "test_version"

func TestShowhelp(t *testing.T) {
	args := []string{
		"get",
		"-h",
	}

	p := New()
	_, err := p.parseOptions(args, version)
	assert.NotNil(t, err)

	args = []string{
		"get",
		"--help",
	}

	p = New()
	_, err = p.parseOptions(args, version)
	assert.NotNil(t, err)
}

func TestResumeDifferentProcs(t *testing.T) {
	// listening file server
	mux := http.NewServeMux()
	mux.HandleFunc("/file.name", func(w http.ResponseWriter, r *http.Request) {
		fp := filepath.Join("_testdata", "test.tar.gz")
		data, err := os.ReadFile(fp)
		if err != nil {
			t.Errorf("failed to readfile: %s", err)
		}
		http.ServeContent(w, r, fp, time.Now(), bytes.NewReader(data))
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	targetURL := fmt.Sprintf("%s/%s", ts.URL, "file.name")

	data, err := os.ReadFile(filepath.Join("_testdata", "test.tar.gz"))
	if err != nil {
		t.Fatal(err)
	}
	cl := int64(len(data))

	// -p 1 ile başlayıp -p 2 ile (ve tersi) resume edebilmeli,
	// hepsi için ayrı klasör açılmamalı: tek "_file.name.partial".
	pairs := [][2]int{{1, 2}, {2, 1}, {1, 4}, {4, 1}, {3, 2}}
	for _, tc := range pairs {
		t.Run(fmt.Sprintf("p%d-to-p%d", tc[0], tc[1]), func(t *testing.T) {
			tmpDir := t.TempDir()
			pdir := filepath.Join(tmpDir, "_file.name.partial")
			if err := os.MkdirAll(pdir, 0755); err != nil {
				t.Fatal(err)
			}
			// Simulate an interrupted download made with tc[0]:
			// first chunk complete, second chunk half, rest missing.
			taskSize := cl / int64(tc[0])
			for i := 0; i < tc[0]; i++ {
				off := taskSize * int64(i)
				var end int64
				if i == tc[0]-1 {
					end = cl
				} else {
					end = off + taskSize
				}
				chunk := data[off:end]
				var part []byte
				switch i {
				case 0:
					part = chunk
				case 1:
					part = chunk[:len(chunk)/2]
				default:
					continue
				}
				if err := os.WriteFile(
					filepath.Join(pdir, fmt.Sprintf("file.name.part-%d", off)),
					part, 0644,
				); err != nil {
					t.Fatal(err)
				}
			}
			p := New()
			if err := p.Run(context.Background(), version, []string{
				"get", "-p", fmt.Sprint(tc[1]),
				targetURL, "--timeout", "5", "--output", tmpDir,
			}); err != nil {
				t.Fatalf("failed to Run: %s", err)
			}
			cmpFileChecksum(t,
				filepath.Join("_testdata", "test.tar.gz"),
				filepath.Join(tmpDir, "file.name"),
			)
			entries, _ := os.ReadDir(tmpDir)
			for _, e := range entries {
				if e.IsDir() && e.Name() != "_file.name.partial" {
					t.Errorf("unexpected extra partial dir: %s", e.Name())
				}
			}
		})
	}
}

func TestMain(m *testing.M) {
	stdout = io.Discard
	os.Exit(m.Run())
}

func TestGet(t *testing.T) {
	// listening file server
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/moo", http.StatusFound)
	})

	mux.HandleFunc("/moo", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/mooo", http.StatusFound)
	})

	mux.HandleFunc("/mooo", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/test.tar.gz", http.StatusFound)
	})

	mux.HandleFunc("/test.tar.gz", func(w http.ResponseWriter, r *http.Request) {
		fp := "_testdata/test.tar.gz"
		data, err := os.ReadFile(fp)
		if err != nil {
			t.Errorf("failed to readfile: %s", err)
		}
		http.ServeContent(w, r, fp, time.Now(), bytes.NewReader(data))
	})

	ts := httptest.NewServer(mux)
	defer ts.Close()

	// begin tests
	url := ts.URL

	tmpdir := t.TempDir()

	cfg := &DownloadConfig{
		Filename:      "test.tar.gz",
		ContentLength: 1719652,
		Dirname:       tmpdir,
		Procs:         4,
		URLs:          []string{ts.URL},
		Client:        newDownloadClient(1),
	}

	t.Run("check", func(t *testing.T) {
		target, err := Check(context.Background(), &CheckConfig{
			URLs:    []string{url},
			Timeout: 10 * time.Second,
		})

		if err != nil {
			t.Fatalf("failed to check header: %s", err)
		}

		if len(target.URLs) == 0 {
			t.Fatalf("invalid URL length %d", len(target.URLs))
		}

		// could redirect?
		assert.NotEqual(t, target.URLs[0], url, "failed to get of the last url in the redirect")
	})

	t.Run("download", func(t *testing.T) {
		err := Download(context.Background(), cfg)
		if err != nil {
			t.Fatal(err)
		}
		// check the partial dir is cleaned up after bind
		partialDir := filepath.Join(tmpdir, "_test.tar.gz.partial")
		if _, err := os.Stat(partialDir); !os.IsNotExist(err) {
			t.Errorf("partial dir %q should have been removed after bind", partialDir)
		}

		cmpFileChecksum(t, "_testdata/test.tar.gz", filepath.Join(tmpdir, cfg.Filename))
	})
}

func get2md5(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}

	defer f.Close()

	hash := md5.New()
	if _, err := io.Copy(hash, f); err != nil {
		return "", err
	}

	// get the 16 bytes hash
	bytes := hash.Sum(nil)[:16]

	return hex.EncodeToString(bytes), nil
}

func cmpFileChecksum(t *testing.T, wantPath, gotPath string) {
	t.Helper()
	want, err := get2md5(wantPath)

	if err != nil {
		t.Fatalf("failed to md5sum of original file: %s", err)
	}

	resultfp, err := get2md5(gotPath)
	if err != nil {
		t.Fatalf("failed to md5sum of result file: %s", err)
	}

	if want != resultfp {
		t.Errorf("expected %s got %s", want, resultfp)
	}
}

func TestErrors(t *testing.T) {
	err := errors.New("first")
	err = errors.Wrap(err, "second")
	err = errors.Wrap(err, "third")

	err = errTop(err)
	if err.Error() != "first" {
		t.Errorf("could not get top message")
	}
}
