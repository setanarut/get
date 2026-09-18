# get

A multi-connection file downloader using parallel HTTP range requests.
Downloads are resumable and multiple mirror URLs can be used at once.

```
> get https://example.com/file.zip
63.97 MiB / 231.82 MiB  █████████░░░░░░░░░░░░░░░░░░░░░░  27.59% 1.80 MiB p/s ETA 1m33s
```

```
Usage:
  get [flags] URL...
Flags:
  -h, --help                Help for get
  -o, --output string       Output file to <filename>
  -p, --procs int           The number of connections for a single URL (default 1)
  -r, --referer string      Identify as <referer>
  -t, --timeout int         Set timeout of checking request in seconds (default 10)
  -u, --user-agent string   Identify as <agent>
  -v, --version             Version for get
```

## Install

Build from source (requires Go 1.27.1+):

```
go install github.com/setanarut/get/cmd/get@latest
```

## Usage

Download a file with 4 connections:

```
get -p 4 https://example.com/file.zip
```

Save to a specific path:

```
get -o ./downloads/file.zip https://example.com/file.zip
```

Download from multiple mirrors at the same time:

```
get -p 2 https://mirror-a.com/file.zip https://mirror-b.com/file.zip
```

URLs can also be passed through stdin, one per line.

## Resume

If a download is interrupted (`CTRL+C`), run the same command again. The downloaded
chunks are stored in a `_<filename>.partial` directory next to the output
file and reused on the next run.

```
> get https://example.com/file.zip
73.17 MiB / 231.82 MiB  ████████████░░░░░░░░░░░░░░░░░░░░░░░  31.56% 1.78 MiB p/s ETA 1m28s
^C
> get https://example.com/file.zip
94.28 MiB / 231.82 MiB  ███████████████░░░░░░░░░░░░░░░░░░░░  40.67% 1.76 MiB p/s ETA 1m17s
^C
```

Because chunks are stored by their byte offset, you can resume with a
different `-p` value and the already downloaded data is still reused.

## Notes

- The server must support HTTP range requests (`Accept-Ranges: bytes`).
- Using many connections to a single URL can put load on the server. In
  most cases `4` or fewer is enough.