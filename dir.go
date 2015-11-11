// Copyright (c) 2009 Google Inc. All rights reserved.
//
// Redistribution and use in source and binary forms, with or without
// modification, are permitted provided that the following conditions are
// met:
//
//    * Redistributions of source code must retain the above copyright
// notice, this list of conditions and the following disclaimer.
//    * Redistributions in binary form must reproduce the above
// copyright notice, this list of conditions and the following disclaimer
// in the documentation and/or other materials provided with the
// distribution.
//    * Neither the name of Google Inc. nor the names of its
// contributors may be used to endorse or promote products derived from
// this software without specific prior written permission.
//
// THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
// "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
// LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
// A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
// OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
// SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
// LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
// DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
// THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
// (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
// OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
//
// Subject to the terms and conditions of this License, Google hereby
// grants to You a perpetual, worldwide, non-exclusive, no-charge,
// royalty-free, irrevocable (except as stated in this section) patent
// license to make, have made, use, offer to sell, sell, import, and
// otherwise transfer this implementation of Go, where such license
// applies only to those patent claims licensable by Google that are
// necessarily infringed by use of this implementation of Go. If You
// institute patent litigation against any entity (including a
// cross-claim or counterclaim in a lawsuit) alleging that this
// implementation of Go or a Contribution incorporated within this
// implementation of Go constitutes direct or contributory patent
// infringement, then any patent licenses granted to You under this
// License for this implementation of Go shall terminate as of the date
// such litigation is filed.
package vufs

import (
	"fmt"
	"strconv"
)

type ProtocolError string

func (e ProtocolError) Error() string {
	return string(e)
}

const (
	STATMAX = 65535
)

type Dir struct {
	Type   uint16
	Dev    uint32
	Qid    Qid
	Mode   Perm
	Atime  uint32
	Mtime  uint32
	Length uint64
	Name   string
	Uid    string
	Gid    string
	Muid   string
}

var nullDir = Dir{
	^uint16(0),
	^uint32(0),
	Qid{^uint64(0), ^uint32(0), ^uint8(0)},
	^Perm(0),
	^uint32(0),
	^uint32(0),
	^uint64(0),
	"",
	"",
	"",
	"",
}

func (d *Dir) Null() {
	*d = nullDir
}

func pdir(b []byte, d *Dir) []byte {
	n := len(b)
	b = pbit16(b, 0) // length, filled in later
	b = pbit16(b, d.Type)
	b = pbit32(b, d.Dev)
	b = pqid(b, d.Qid)
	b = pperm(b, d.Mode)
	b = pbit32(b, d.Atime)
	b = pbit32(b, d.Mtime)
	b = pbit64(b, d.Length)
	b = pstring(b, d.Name)
	b = pstring(b, d.Uid)
	b = pstring(b, d.Gid)
	b = pstring(b, d.Muid)
	pbit16(b[0:n], uint16(len(b)-(n+2)))
	return b
}

func (d *Dir) Bytes() ([]byte, error) {
	return pdir(nil, d), nil
}

func UnmarshalDir(b []byte) (d *Dir, err error) {
	defer func() {
		if v := recover(); v != nil {
			d = nil
			err = ProtocolError("malformed Dir")
		}
	}()

	n, b := gbit16(b)
	if int(n) != len(b) {
		panic(1)
	}

	d = new(Dir)
	d.Type, b = gbit16(b)
	d.Dev, b = gbit32(b)
	d.Qid, b = gqid(b)
	d.Mode, b = gperm(b)
	d.Atime, b = gbit32(b)
	d.Mtime, b = gbit32(b)
	d.Length, b = gbit64(b)
	d.Name, b = gstring(b)
	d.Uid, b = gstring(b)
	d.Gid, b = gstring(b)
	d.Muid, b = gstring(b)

	if len(b) != 0 {
		panic(1)
	}
	return d, nil
}

func (d *Dir) String() string {
	return fmt.Sprintf("'%s' '%s' '%s' '%s' q %v m %#o at %d mt %d l %d t %d d %d",
		d.Name, d.Uid, d.Gid, d.Muid, d.Qid, d.Mode,
		d.Atime, d.Mtime, d.Length, d.Type, d.Dev)
}

func dumpsome(b []byte) string {
	if len(b) > 64 {
		b = b[0:64]
	}

	printable := true
	for _, c := range b {
		if c != 0 && c < 32 || c > 127 {
			printable = false
			break
		}
	}

	if printable {
		return strconv.Quote(string(b))
	}
	return fmt.Sprintf("%x", b)
}

type Perm uint32

type permChar struct {
	bit Perm
	c   int
}

var permChars = []permChar{
	permChar{DMDIR, 'd'},
	permChar{DMAPPEND, 'a'},
	permChar{DMAUTH, 'A'},
	permChar{DMDEVICE, 'D'},
	permChar{DMSOCKET, 'S'},
	permChar{DMNAMEDPIPE, 'P'},
	permChar{0, '-'},
	permChar{DMEXCL, 'l'},
	permChar{DMSYMLINK, 'L'},
	permChar{0, '-'},
	permChar{0400, 'r'},
	permChar{0, '-'},
	permChar{0200, 'w'},
	permChar{0, '-'},
	permChar{0100, 'x'},
	permChar{0, '-'},
	permChar{0040, 'r'},
	permChar{0, '-'},
	permChar{0020, 'w'},
	permChar{0, '-'},
	permChar{0010, 'x'},
	permChar{0, '-'},
	permChar{0004, 'r'},
	permChar{0, '-'},
	permChar{0002, 'w'},
	permChar{0, '-'},
	permChar{0001, 'x'},
	permChar{0, '-'},
}

func (p Perm) String() string {
	s := ""
	did := false
	for _, pc := range permChars {
		if p&pc.bit != 0 {
			did = true
			s += string(pc.c)
		}
		if pc.bit == 0 {
			if !did {
				s += string(pc.c)
			}
			did = false
		}
	}
	return s
}

func gperm(b []byte) (Perm, []byte) {
	p, b := gbit32(b)
	return Perm(p), b
}

func pperm(b []byte, p Perm) []byte {
	return pbit32(b, uint32(p))
}

type Qid struct {
	Path uint64
	Vers uint32
	// The type of the file, represented as a bit vector corresponding
         // to the high 8 bits of the file's mode word.
	Type uint8
}

func (q Qid) String() string {
	t := ""
	if q.Type&QTDIR != 0 {
		t += "d"
	}
	if q.Type&QTAPPEND != 0 {
		t += "a"
	}
	if q.Type&QTEXCL != 0 {
		t += "l"
	}
	if q.Type&QTAUTH != 0 {
		t += "A"
	}
	return fmt.Sprintf("(%.16x %d %s)", q.Path, q.Vers, t)
}

func (q Qid) Reset() {
	q.Path = 0
	q.Vers = 0
	q.Type = 0
}

func gqid(b []byte) (Qid, []byte) {
	var q Qid
	q.Type, b = gbit8(b)
	q.Vers, b = gbit32(b)
	q.Path, b = gbit64(b)
	return q, b
}

func pqid(b []byte, q Qid) []byte {
	b = pbit8(b, q.Type)
	b = pbit32(b, q.Vers)
	b = pbit64(b, q.Path)
	return b
}
