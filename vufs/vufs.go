// Copyright 2009 The go9p Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"flag"
	"github.com/mbucc/vufs"
	"log"
	"os"
)

var addr = flag.String("addr", vufs.DEFAULTPORT, "network address")
var debug = flag.Int("debug", 0, "print debug messages")
var root = flag.String("root", "/", "root filesystem")

func main() {

	flag.Parse()

	fs := vufs.New(*root)

	if *debug != 0 {
		fs.Chatty(true)
	}

	err := fs.Start("tcp", *addr)

	if err != nil {
		log.Println(err)
		os.Exit(1)
	}

}
