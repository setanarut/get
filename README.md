# get

A multi-connection file downloader using parallel HTTP range requests.

- Fast: downloads a file using multiple connections
- Resumable: keeps downloaded chunks and continues where it left off
- Mirror-aware: can download from multiple URLs at once
- Cross-platform: builds on Windows, Linux and macOS

## Install

Build from source (requires Go 1.27.1+):

```sh
go install github.com/setanarut/get/cmd/get@latest
```

## Usage

```text
Usage: get [options] URL
  Options:
  -h,  --help                   print usage and exit
  -p,  --procs <num>            the number of connections for a single URL (default 1)
  -o,  --output <filename>      output file to <filename>
  -t,  --timeout <seconds>      timeout of checking request in seconds (default 10s)
  -u,  --user-agent <agent>     identify as <agent>
  -r,  --referer <referer>      identify as <referer>
```

### Examples

Download a file with 4 connections:

```sh
get -p 4 https://example.com/file.tar.gz
```

Save to a specific path:

```sh
get -o ./downloads/file.tar.gz https://example.com/file.tar.gz
```

Download from multiple mirrors at the same time:

```sh
get -p 2 https://mirror-a.com/file.tar.gz https://mirror-b.com/file.tar.gz
```

URLs can also be passed through stdin, one per line.

## Resume

If a download is interrupted, run the same command again. The downloaded
chunks are stored in a `_<filename>.partial` directory next to the output
file and reused on the next run.

Because chunks are stored by their byte offset, you can resume with a
different `-p` value and the already downloaded data is still reused.

## Notes

- The server must support HTTP range requests (`Accept-Ranges: bytes`).
- Using many connections to a single URL can put load on the server. In
  most cases `4` or fewer is enough.