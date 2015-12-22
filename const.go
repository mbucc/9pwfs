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

const (
	VERSION9P = "9P2000"
	MAXWELEM  = 16

	OREAD     = 0
	OWRITE    = 1
	ORDWR     = 2
	OEXEC     = 3
	OTRUNC    = 16
	OCEXEC    = 32
	ORCLOSE   = 64
	ODIRECT   = 128
	//ONONBLOCK = 256  I don't see any mention of blocking Plan 9 man pages.
	OEXCL     = 0x1000
	OLOCK     = 0x2000
	OAPPEND   = 0x4000

	AEXIST = 0
	AEXEC  = 1
	AWRITE = 2
	AREAD  = 4

	QTDIR     = 0x80
	QTAPPEND  = 0x40
	QTEXCL    = 0x20
	QTMOUNT   = 0x10
	QTAUTH    = 0x08
	QTTMP     = 0x04
	QTSYMLINK = 0x02
	QTFILE    = 0x00

	DMDIR       = 0x80000000
	DMAPPEND    = 0x40000000
	DMEXCL      = 0x20000000
	DMMOUNT     = 0x10000000
	DMAUTH      = 0x08000000
	DMTMP       = 0x04000000
	DMSYMLINK   = 0x02000000
	DMDEVICE    = 0x00800000
	DMNAMEDPIPE = 0x00200000
	DMSOCKET    = 0x00100000
	DMSETUID    = 0x00080000
	DMSETGID    = 0x00040000
	DMREAD      = 0x4
	DMWRITE     = 0x2
	DMEXEC      = 0x1

	NOTAG   = 0xffff
	NOFID   = 0xffffffff
	NOUID   = 0xffffffff
	IOHDRSZ = 24

	DEFAULTPORT = ":5001"
	MAX_MSIZE = 131072
	DEFAULT_USER = "adm"
)
