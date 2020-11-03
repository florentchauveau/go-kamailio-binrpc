# go-kamailio-binrpc
[![Go Report Card](https://goreportcard.com/badge/github.com/florentchauveau/go-kamailio-binrpc)](https://goreportcard.com/report/github.com/florentchauveau/go-kamailio-binrpc)
[![CircleCI](https://circleci.com/gh/florentchauveau/go-kamailio-binrpc.svg?style=shield)](https://circleci.com/gh/florentchauveau/go-kamailio-binrpc)
[![GoDoc](https://pkg.go.dev/badge/github.com/florentchauveau/go-kamailio-binrpc/v3)](https://pkg.go.dev/github.com/florentchauveau/go-kamailio-binrpc/v3)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](https://github.com/florentchauveau/go-kamailio-binrpc/blob/master/LICENSE)

Zero dependency Go implementation of Kamailio BINRPC protocol, for invoking RPC functions.

This library works with any Kamailio version.

go-kamailio-binrpc has been tested with Go 1.11, but should work with previous versions.

## Usage

```go
import binrpc "github.com/florentchauveau/go-kamailio-binrpc/v3"
```

### Full Example

```go
package main

import (
	"fmt"
	"net"

	binrpc "github.com/florentchauveau/go-kamailio-binrpc/v3"
)

func main() {
	// establish connection to Kamailio server
	conn, err := net.Dial("tcp", "localhost:2049")

	if err != nil {
		panic(err)
	}

	// WritePacket returns the cookie generated
	cookie, err := binrpc.WritePacket(conn, "tm.stats")
	
	// for commands that require args, add them as function args
	// like this: binrpc.WritePacket(conn, "stats.fetch", "all")

	if err != nil {
		panic(err)
	}

	// the cookie is passed again for verification
	// we receive records in response
	records, err := binrpc.ReadPacket(conn, cookie)

	if err != nil {
		panic(err)
	}

	// "tm.stats" returns one record that is a struct
	// and all items are int values
	items, _ := records[0].StructItems()

	for _, item := range items {
		value, _ := item.Value.Int()

		fmt.Printf("%s = %d\n",
			item.Key,
			value,
		)
	}
}
```

### Kamailio Config

The `ctl` module must be loaded:

```
loadmodule "ctl.so"
```

If you are using `kamcmd` (and you most probably are), the module is already loaded.

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
