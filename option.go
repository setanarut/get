package get

import (
	"bytes"
	"fmt"

	"github.com/jessevdk/go-flags"
	"github.com/pkg/errors"
)

// Options struct for parse command line arguments
type Options struct {
	Help          bool   `short:"h" long:"help"`
	NumConnection int    `short:"p" long:"procs" default:"1"`
	Output        string `short:"o" long:"output"`
	Timeout       int    `short:"t" long:"timeout" default:"10"`
	UserAgent     string `short:"u" long:"user-agent"`
	Referer       string `short:"r" long:"referer"`
}

func (opts *Options) parse(argv []string, version string) ([]string, error) {
	p := flags.NewParser(opts, flags.PrintErrors)
	args, err := p.ParseArgs(argv)

	if err != nil {
		// Hata durumunda usage YAZDIRMA; sadece hatayı dön.
		return nil, errors.Wrap(err, "invalid command line options")
	}

	return args, nil
}

func (opts Options) usage(version string) []byte {
	buf := bytes.Buffer{}

	fmt.Fprintf(&buf,
		"Get, file download client %s\n"+
			`Usage: get [options] URL
  Options:
  -h,  --help                   Print usage and exit
  -p,  --procs <num>            The number of connections for a single URL (default 1)
  -o,  --output <filename>      Output file to <filename>
  -t,  --timeout <seconds>      Timeout of checking request in seconds (default 10s)
  -u,  --user-agent <agent>     Identify as <agent>
  -r,  --referer <referer>      Identify as <referer>
`, version)
	return buf.Bytes()
}
