package get

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Songmu/prompter"
	"github.com/asaskevich/govalidator"
	"github.com/pkg/errors"
)

// Get structs
type Get struct {
	Output string
	Procs  int
	URLs   []string

	args      []string
	timeout   int
	useragent string
	referer   string
}

// New for Get package
func New() *Get {
	return &Get{
		Procs:   runtime.NumCPU(), // default
		timeout: 10,
	}
}

// Run execute methods in Get package
func (Get *Get) Run(ctx context.Context, version string, args []string) error {
	if err := Get.Ready(version, args); err != nil {
		return errTop(err)
	}

	// TODO(codehex): calc maxIdleConnsPerHost
	client := newDownloadClient(16)

	target, err := Check(ctx, &CheckConfig{
		URLs:    Get.URLs,
		Timeout: time.Duration(Get.timeout) * time.Second,
		Client:  client,
	})
	if err != nil {
		return err
	}

	filename := target.Filename

	var dir string
	if Get.Output != "" {
		fi, err := os.Stat(Get.Output)
		if err == nil && fi.IsDir() {
			dir = Get.Output
		} else {
			dir, filename = filepath.Split(Get.Output)
			if dir != "" {
				if err := os.MkdirAll(dir, 0755); err != nil {
					return errors.Wrapf(err, "failed to create diretory at %s", dir)
				}
			}
		}
	}

	opts := []DownloadOption{
		WithUserAgent(Get.useragent, version),
		WithReferer(Get.referer),
	}

	return Download(ctx, &DownloadConfig{
		Filename:      filename,
		Dirname:       dir,
		ContentLength: target.ContentLength,
		Procs:         Get.Procs,
		URLs:          target.URLs,
		Client:        client,
	}, opts...)
}

const (
	warningNumConnection = 4
	warningMessage       = "[WARNING] Using a large number of connections to 1 URL can lead to DOS attacks.\n" +
		"In most cases, `4` or less is enough. In addition, the case is increasing that if you use multiple connections to 1 URL does not increase the download speed with the spread of CDNs.\n" +
		"See: https://github.com/emaballarin/Get#disclaimer\n" +
		"\n" +
		"Would you execute knowing these?\n"
)

// Ready method define the variables required to Download.
func (Get *Get) Ready(version string, args []string) error {
	opts, err := Get.parseOptions(args, version)
	if err != nil {
		return errors.Wrap(errTop(err), "failed to parse command line args")
	}

	if opts.Timeout > 0 {
		Get.timeout = opts.Timeout
	}

	if err := Get.parseURLs(); err != nil {
		return errors.Wrap(err, "failed to parse of url")
	}

	if opts.NumConnection > warningNumConnection && !prompter.YN(warningMessage, false) {
		return makeIgnoreErr()
	}

	Get.Procs = opts.NumConnection * len(Get.URLs)

	if opts.Output != "" {
		Get.Output = opts.Output
	}

	if opts.UserAgent != "" {
		Get.useragent = opts.UserAgent
	}

	if opts.Referer != "" {
		Get.referer = opts.Referer
	}

	return nil
}

func (Get *Get) parseOptions(argv []string, version string) (*Options, error) {
	var opts Options

	// Argüman yoksa: usage YAZDIRMA, sadece hatayı dön.
	if len(argv) == 0 {
		return nil, errors.New("URL is required at least one")
	}

	o, err := opts.parse(argv, version)
	if err != nil {
		return nil, err
	}

	// Sadece -h / --help verilmişse usage yazdır.
	if opts.Help {
		stdout.Write(opts.usage(version))
		return nil, makeIgnoreErr()
	}

	Get.args = o

	return &opts, nil
}

func (Get *Get) parseURLs() error {
	// find url in args
	for _, argv := range Get.args {
		if govalidator.IsURL(argv) {
			Get.URLs = append(Get.URLs, argv)
		}
	}

	if len(Get.URLs) < 1 {
		fmt.Fprintf(stdout, "Please input url separate with space or newline\n")
		fmt.Fprintf(stdout, "Start download with ^D\n")

		// scanning url from stdin
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			scan := scanner.Text()
			urls := strings.SplitSeq(scan, " ")
			for url := range urls {
				if govalidator.IsURL(url) {
					Get.URLs = append(Get.URLs, url)
				}
			}
		}

		if err := scanner.Err(); err != nil {
			return errors.Wrap(err, "failed to parse url from stdin")
		}

		if len(Get.URLs) < 1 {
			return errors.New("urls not found in the arguments passed")
		}
	}

	return nil
}
