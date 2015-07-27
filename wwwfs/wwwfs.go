// Copyright 2009 The go9p Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"flag"
	"fmt"
	"github.com/mbucc/wwwfs"
	"log"
)

var addr = flag.String("addr", ":5640", "network address")
var debug = flag.Int("debug", 0, "print debug messages")
var root = flag.String("root", "/", "root filesystem")

func main() {
	flag.Parse()
	ufs := new(wwwfs.WwwFs)
	ufs.Id = "wwwfs"
	ufs.Root = *root
	ufs.Debuglevel = *debug
	ufs.Start(ufs)

	fmt.Print("wwwfs starting\n")
	// determined by build tags
	//extraFuncs()
	err := ufs.StartNetListener("tcp", *addr)
	if err != nil {
		log.Println(err)
	}
}
