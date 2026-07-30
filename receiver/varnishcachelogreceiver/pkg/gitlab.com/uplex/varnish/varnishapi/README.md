# varnishapi

`varnishapi` is a Go module and toolset for interacting with Varnish Cache, a high-performance HTTP accelerator. It provides command-line utilities and Go packages to administer Varnish instances, process logs, retrieve statistics, and access shared memory data. This module wraps Varnish's C API using cgo, enabling programmatic control and monitoring in Go applications.

## Features

- **Command-Line Tools**:
  - `govarnishadm`: Administer Varnish via CLI commands (e.g., status, VCL management).
  - `govarnishlog`: Parse and filter Varnish logs with grouping and querying.
  - `govarnishstat`: Retrieve and display Varnish statistics in various formats (e.g., JSON, HTML table, real-time web interface).
- **Go Packages**:
  - `admin`: Handle administrative commands and responses.
  - `log`: Parse log records, cursors, and queries with support for transactions and payloads.
  - `stats`: Access Varnish statistics, descriptions, and real-time data.
  - `vsm`: Interface with Varnish Shared Memory (VSM) for attachment and status checks.
- Supports attachment to running Varnish instances or log files.

## Installation

### Prerequisites
- Go 1.18 or later.
- Varnish Cache installed on your system (with development headers for cgo compilation).

## Usage

To use as a library in your Go project:

go get gitlab.com/uplex/varnish/varnishapi

### Command-Line Tools

#### govarnishadm

Administer a Varnish instance via its CLI interface.


- Check Varnish status

```sh
./govarnishadm -T localhost:6082 status
```

- Load a new VCL configuration

```sh
./govarnishadm -T localhost:6082 vcl.load myconfig /path/to/config.vcl
```

- Set a VCL as active

```sh
./govarnishadm -T localhost:6082 vcl.use myconfig
```

Options:

- `-T <address>`: Management address (default: from Varnish).

- `-S <secret>`: Path to secret file for authentication.

- `-t <timeout>`: Timeout in seconds.

#### govarnishlog

Process and filter Varnish logs.

- Tail logs with request grouping

```sh
./govarnishlog -g request
```

- Query logs for specific tags

```sh
./govarnishlog -q "ReqURL ~ /api"
```

- Read from a log file

```sh
./govarnishlog -r /var/log/varnish.log
```

Options:

- `-g <grouping>`: Grouping mode (e.g., `request`, `vxid`).

- `-q <query>`: Filter query (e.g., tag-based expressions).

- `-r <file>`: Read from file instead of shared memory.

#### govarnishstat

Display Varnish statistics.

- Show all stats in JSON format

```sh
./govarnishstat -j
```

- Filter stats by glob pattern

```sh
./govarnishstat -f "cache_*"
```

- Output as HTML table

```sh
./govarnishstat -H
```

Options:

- `-j`: JSON output.

- `-f <pattern>`: Include/exclude fields via glob.

- `-H`: HTML table output.

### Using as a Go Library

Import the packages in your Go code:

```go
import (
"gitlab.com/uplex/varnish/varnishapi/pkg/admin"
"gitlab.com/uplex/varnish/varnishapi/pkg/log"
"gitlab.com/uplex/varnish/varnishapi/pkg/stats"
"gitlab.com/uplex/varnish/varnishapi/pkg/vsm"
)
```

Example: Attach to Varnish and read stats.

```go
package main

import (
"fmt"
"gitlab.com/uplex/varnish/varnishapi/pkg/stats"
)

func main() {
    s := stats.New()
    defer s.Release()

    if err := s.Attach(""); err != nil {
        panic(err)
    }

    // Read all stats
    err := s.Read(func(name string, val uint64) bool {
        fmt.Printf("%s: %d\n", name, val)
        return true // Continue reading
    })

    if err != nil {
        panic(err)
    }
}
```

For logs:

```go
package main

import (
"fmt"
"gitlab.com/uplex/varnish/varnishapi/pkg/log"
)

func main() {
    l := log.New()
    if err := l.Attach(""); err != nil {
        panic(err)
    }

    cursor, err := l.NewCursor()
    if err != nil {
        panic(err)
    }

    for {
        status := cursor.Next()
        if status != log.More {
            break
        }
        rec := cursor.Record()
        fmt.Printf("Tag: %s, Payload: %s\n", rec.Tag, rec.Payload)
    }
}
```

## API Overview

- **admin**: Provides `Admin` struct for CLI commands, status checks, and VCL management. Handles responses and errors.

- **log**: Includes `Log`, `Cursor`, and `Query` for log processing. Supports grouping, parsing, and transaction types.

- **stats**: Offers `Stats` for retrieving counters, gauges, and descriptions. Supports real-time reading via handlers.

- **vsm**: Manages Varnish Shared Memory attachment, timeouts, and status queries.

Refer to the Go documentation (`go doc`) or source code for detailed function signatures.

## Testing

Run tests and benchmarks:

```sh
go test ./...
go test -bench=. ./pkg/stats
```

End-to-end tests require a running Varnish instance.

## Contributing

Contributions are welcome! Please:

1. Fork the repository.

2. Create a feature branch.

3. Add tests for new functionality.

4. Submit a pull request.

## License

This project is licensed under the BSD License. See `LICENSE` for details.

## Support

For issues or questions, open a GitLab issue or check the Varnish documentation at [varnish-cache.org](https://varnish-cache.org).
