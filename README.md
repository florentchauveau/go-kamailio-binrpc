# go-kamailio-binrpc
[![Go Report Card](https://goreportcard.com/badge/github.com/florentchauveau/go-kamailio-binrpc)](https://goreportcard.com/report/github.com/florentchauveau/go-kamailio-binrpc)
[![CircleCI](https://circleci.com/gh/florentchauveau/go-kamailio-binrpc.svg?style=shield)](https://circleci.com/gh/florentchauveau/go-kamailio-binrpc)
[![GoDoc](https://godoc.org/github.com/florentchauveau/go-kamailio-binrpc?status.svg)](https://godoc.org/github.com/florentchauveau/go-kamailio-binrpc)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](https://github.com/florentchauveau/go-kamailio-binrpc/blob/master/LICENSE)

Go implementation of Kamailio BINRPC protocol for invoking RPC functions.

This library works with any Kamailio version.

go-kamailio-binrpc has been tested with Go 1.11, but should work with previous versions.

## Usage

### Code

```go
package main

import (
	"fmt"
	"net"

	binrpc "github.com/florentchauveau/go-kamailio-binrpc"
)

func main() {
	// establish connection to Kamailio server
	conn, err := net.Dial("tcp", "localhost:2049")

	if err != nil {
		panic(err)
	}

	// WritePacket returns the cookie generated
	cookie, err := binrpc.WritePacket(conn, "tm.stats")

	if err != nil {
		panic(err)
	}

	// the cookie is passed again for verification
	// we receive records in response
	records, err := binrpc.ReadPacket(conn, cookie)

	if err != nil {
		panic(err)
	}

	// "tm.stats" returns one record that is a map
	// with at least "total" and "current" keys
	avpMap, _ := records[0].Map()

	total, _ := avpMap["total"].Int()
	current, _ := avpMap["current"].Int()

	fmt.Printf("total = %d\ncurrent = %d\n",
		total,
		current,
	)
}
```

### Kamailio config

The `ctl` module must be loaded:

```
loadmodule "ctl.so"
```

In order to connect remotely, you must listen on TCP or UDP (defaults to local unix socket):

```
modparam("ctl", "binrpc", "tcp:2049")
```

**WARNING**: this will open your Kamailio to the world. Make sure you have a firewall in place, or listen on an internal interface.


## Limits

For now, only int, string and structs are implemented. Other types will return an error.

## Contributing

Contributions are welcome.

## License

This library is distributed under the [MIT](https://github.com/florentchauveau/go-kamailio-binrpc/blob/master/LICENSE) license.
