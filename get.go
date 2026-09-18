package get

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Songmu/prompter"
	"github.com/asaskevich/govalidator"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

const (
	warningNumConnection = 4
	warningMessage       = "[WARNING] Using a large number of connections to 1 URL can lead to DOS attacks.\n" +
		"In most cases, `4` or less is enough. In addition, the case is increasing that if you use multiple connections to 1 URL does not increase the download speed with the spread of CDNs.\n" +
		"\n" +
		"Would you execute knowing these?\n"

	defaultTimeout = 10 // seconds
)

// Get struct
type Get struct {
	Output string
	Procs  int
	URLs   []string

	numConnection int
	timeout       int
	useragent     string
	referer       string
}

// New for Get package
func New() *Get {
	return &Get{
		Procs:   runtime.NumCPU(), // default
		timeout: defaultTimeout,
	}
}

// Run parses the command line arguments with cobra and executes the download.
// With -h / --help, cobra prints the usage and returns nil without running
// the download, so no "URL is required at least one" error is returned.
func (g *Get) Run(ctx context.Context, version string, args []string) error {
	cmd := g.newCommand(ctx, version)
	cmd.SetOut(stdout)
	cmd.SetArgs(args)
	return cmd.Execute()
}

// newCommand builds the cobra command and binds all flags to g.
// The -h/--help and -v/--version flags are added manually instead of
// relying on the ones cobra adds automatically, so their usage text and
// version output can be customized. When the flags are already defined,
// cobra skips adding its own defaults.
func (g *Get) newCommand(ctx context.Context, version string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get [flags] URL...",
		Short: "Get, file download client",
		Long: `Get, file download client.

A multi-connection file downloader using parallel HTTP range requests.
Downloads are resumable and multiple mirror URLs can be used at once.`,
		Version: version,
		Example: `  get -p 4 https://example.com/file.zip
  get -o ./downloads/file.zip https://example.com/file.zip
  get -p 2 https://mirror-a.com/file.zip https://mirror-b.com/file.zip`,
		Args: cobra.ArbitraryArgs,
		// Do not print usage on errors; errors are printed by the caller.
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return g.run(ctx, version, args)
		},
	}

	flags := cmd.Flags()
	flags.IntVarP(&g.numConnection, "procs", "p", 1, "The number of connections for a single URL")
	flags.StringVarP(&g.Output, "output", "o", "", "Output file to <filename>")
	flags.IntVarP(&g.timeout, "timeout", "t", defaultTimeout, "Set timeout of checking request in seconds")
	flags.StringVarP(&g.useragent, "user-agent", "u", "", "Identify as <agent>")
	flags.StringVarP(&g.referer, "referer", "r", "", "Identify as <referer>")

	// -h/--help and -v/--version are added manually; cobra still handles
	// them, but we control the usage texts.
	flags.BoolP("help", "h", false, "Help for get")
	flags.BoolP("version", "v", false, "Version for get")

	// Print "get v1.1.0" instead of cobra's default "get version v1.1.0".
	cmd.SetVersionTemplate(`{{with .DisplayName}}{{printf "%s " .}}{{end}}{{printf "%s\n" .Version}}`)

	return cmd
}

// run is executed by cobra after flag parsing; it is never called when
// only -h/--help (or -v/--version) is given.
func (g *Get) run(ctx context.Context, version string, args []string) error {
	if err := g.parseURLs(args); err != nil {
		return err
	}

	// Same as the previous behavior: a non positive timeout falls back
	// to the default instead of expiring instantly.
	if g.timeout <= 0 {
		g.timeout = defaultTimeout
	}

	if g.numConnection > warningNumConnection && !prompter.YN(warningMessage, false) {
		return nil
	}

	g.Procs = g.numConnection * len(g.URLs)

	// TODO(codehex): calc maxIdleConnsPerHost
	client := newDownloadClient(16)

	target, err := Check(ctx, &CheckConfig{
		URLs:    g.URLs,
		Timeout: time.Duration(g.timeout) * time.Second,
		Client:  client,
	})
	if err != nil {
		return err
	}

	filename := target.Filename

	var dir string
	if g.Output != "" {
		fi, err := os.Stat(g.Output)
		if err == nil && fi.IsDir() {
			dir = g.Output
		} else {
			dir, filename = filepath.Split(g.Output)
			if dir != "" {
				if err := os.MkdirAll(dir, 0755); err != nil {
					return errors.Wrapf(err, "failed to create directory at %s", dir)
				}
			}
		}
	}

	opts := []DownloadOption{
		WithUserAgent(g.useragent, version),
		WithReferer(g.referer),
	}

	return Download(ctx, &DownloadConfig{
		Filename:      filename,
		Dirname:       dir,
		ContentLength: target.ContentLength,
		Procs:         g.Procs,
		URLs:          target.URLs,
		Client:        client,
	}, opts...)
}

// parseURLs collects URLs from the positional arguments. If no URL is found
// there, URLs are scanned from stdin (separated by spaces or newlines) only
// when stdin is piped. On an interactive terminal it returns an error right
// away instead of blocking on input.
func (g *Get) parseURLs(args []string) error {
	// find url in args
	for _, argv := range args {
		if govalidator.IsURL(argv) {
			g.URLs = append(g.URLs, argv)
		}
	}

	if len(g.URLs) < 1 {
		if fi, err := os.Stdin.Stat(); err != nil || fi.Mode()&os.ModeCharDevice != 0 {
			return errors.New("url is required, at least one")
		}

		// scanning url from piped stdin
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			scan := scanner.Text()
			urls := strings.SplitSeq(scan, " ")
			for url := range urls {
				if govalidator.IsURL(url) {
					g.URLs = append(g.URLs, url)
				}
			}
		}

		if err := scanner.Err(); err != nil {
			return errors.Wrap(err, "failed to parse url from stdin")
		}

		if len(g.URLs) < 1 {
			return errors.New("urls not found in the arguments passed")
		}
	}

	return nil
}
