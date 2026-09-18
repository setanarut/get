# get

A multi-connection file downloader using parallel HTTP range requests.

```
get -p 4 https://example.com/file.zip
 ████████░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░  11% 1.79 MiB/s
```

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

Download a file with 4 connections:

```sh
get -p 4 https://example.com/file.zip
```

Save to a specific path:

```sh
get -o ./downloads/file.zip https://example.com/file.zip
```

Download from multiple mirrors at the same time:

```sh
get -p 2 https://mirror-a.com/file.zip https://mirror-b.com/file.zip
```

URLs can also be passed through stdin, one per line.

## Resume

If a download is interrupted (`CTRL+C`), run the same command again. The downloaded
chunks are stored in a `_<filename>.partial` directory next to the output
file and reused on the next run.

```sh
get https://example.com/file.zip
 ████████░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░  17% 1.77 MiB/s
 ^C
get https://example.com/file.zip
 ███████████░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░  21% 1.74 MiB/s
 ^C
```

Because chunks are stored by their byte offset, you can resume with a
different `-p` value and the already downloaded data is still reused.

## Notes

- The server must support HTTP range requests (`Accept-Ranges: bytes`).
- Using many connections to a single URL can put load on the server. In
  most cases `4` or fewer is enough.