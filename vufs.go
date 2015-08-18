// Copyright 2009 The go9p Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vufs

import (
	"fmt"
	//"github.com/rminnich/go9p"
	"github.com/mbucc/go9p"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/user"
	"path"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const uidgidFile = ".uidgid"

type vufsFid struct {
	path       string
	file       *os.File
	dirs       []os.FileInfo
	direntends []int
	dirents    []byte
	diroffset  uint64
	st         os.FileInfo
	user       string
	group      string
}

type VuFs struct {
	go9p.Srv
	Root string
}

func toError(err error) *go9p.Error {
	var ecode uint32

	ename := err.Error()
	if e, ok := err.(syscall.Errno); ok {
		ecode = uint32(e)
	} else {
		ecode = go9p.EIO
	}

	return &go9p.Error{ename, ecode}
}

// IsBlock reports if the file is a block device
func isBlock(d os.FileInfo) bool {
	stat := d.Sys().(*syscall.Stat_t)
	return (stat.Mode & syscall.S_IFMT) == syscall.S_IFBLK
}

// IsChar reports if the file is a character device
func isChar(d os.FileInfo) bool {
	stat := d.Sys().(*syscall.Stat_t)
	return (stat.Mode & syscall.S_IFMT) == syscall.S_IFCHR
}


func (fid *vufsFid) stat() *go9p.Error {
	var err error

	fid.st, err = os.Lstat(fid.path)
	if err != nil {
		return toError(err)
	}
	return nil
}

func omode2uflags(mode uint8) int {
	ret := int(0)
	switch mode & 3 {
	case go9p.OREAD:
		ret = os.O_RDONLY
		break

	case go9p.ORDWR:
		ret = os.O_RDWR
		break

	case go9p.OWRITE:
		ret = os.O_WRONLY
		break

	case go9p.OEXEC:
		ret = os.O_RDONLY
		break
	}

	if mode&go9p.OTRUNC != 0 {
		ret |= os.O_TRUNC
	}

	return ret
}

func dir2Qid(d os.FileInfo) *go9p.Qid {
	var qid go9p.Qid

	qid.Path = d.Sys().(*syscall.Stat_t).Ino
	qid.Version = uint32(d.ModTime().UnixNano() / 1000000)
	qid.Type = dir2QidType(d)

	return &qid
}

func dir2QidType(d os.FileInfo) uint8 {
	ret := uint8(0)
	if d.IsDir() {
		ret |= go9p.QTDIR
	}

	if d.Mode()&os.ModeSymlink != 0 {
		ret |= go9p.QTSYMLINK
	}

	return ret
}

func dir2Npmode(d os.FileInfo) uint32 {
	ret := uint32(d.Mode() & 0777)
	if d.IsDir() {
		ret |= go9p.DMDIR
	}

	return ret
}

// go9p.Dir is an instantiation of the p.Dir structure
// that can act as a receiver for local methods.
type ufsDir struct {
	go9p.Dir
}

// t.txt-mark-nuts --> t.xt, mark, nuts
// t.txt-mark      --> t.xt, mark, mark
// t.txt           --> t.txt, adm, adm
func path2UidGid(path string) (uid, gid string, err error) {

	// Default owner/group is adm.
	uid = "adm"
	gid = "adm"


	d := "."
	name := path
	n := strings.LastIndex(path, "/")
	if n != -1 {
		d = path[:n]
		name = path[n+1:]
	}


	data, err := ioutil.ReadFile(d + "/" + uidgidFile)
	if err != nil {
		if os.IsNotExist(err) {
			return uid, gid, nil
		} else {
			return "", "", err
		}
	}

	sdata := string(data)
	for _, line := range(strings.Split(sdata, "\n")) {
		columns := strings.Split(line, ":")
		if len(columns) != 3 {
			continue
		}
		if columns[0] == name {
			uid = columns[1]
			gid = columns[2]
			break
		}
	}

	return uid, gid, nil
}

func dir2Dir(path string, d os.FileInfo, upool go9p.Users) (*go9p.Dir, error) {
	if r := recover(); r != nil {
		fmt.Print("stat failed: ", r)
		return nil, &os.PathError{"dir2Dir", path, nil}
	}

	dir := new(ufsDir)
	dir.Qid = *dir2Qid(d)
	dir.Mode = dir2Npmode(d)
	dir.Atime = uint32(0 /*atime(sysMode).Unix()*/)
	dir.Mtime = uint32(d.ModTime().Unix())
	dir.Length = uint64(d.Size())
	dir.Name = path[strings.LastIndex(path, "/")+1:]
	uid, gid, err := path2UidGid(path)
	if err != nil {
		return nil, err
	}
	dir.Uid, dir.Gid = uid, gid

	return &dir.Dir, nil
}

func (*VuFs) ConnOpened(conn *go9p.Conn) {
	if conn.Srv.Debuglevel > 0 {
		log.Println("connected")
	}
}

func (*VuFs) ConnClosed(conn *go9p.Conn) {
	if conn.Srv.Debuglevel > 0 {
		log.Println("disconnected")
	}
}

func (*VuFs) FidDestroy(sfid *go9p.SrvFid) {
	var fid *vufsFid

	if sfid.Aux == nil {
		return
	}

	fid = sfid.Aux.(*vufsFid)
	if fid.file != nil {
		fid.file.Close()
	}
}

func (ufs *VuFs) Attach(req *go9p.SrvReq) {
	if req.Afid != nil {
		req.RespondError(go9p.Enoauth)
		return
	}

	tc := req.Tc
	fid := new(vufsFid)
	// You can think of the ufs.Root as a 'chroot' of a sort.
	// clients attach are not allowed to go outside the
	// directory represented by ufs.Root
	fid.path = path.Join(ufs.Root, tc.Aname)

	req.Fid.Aux = fid
	err := fid.stat()
	if err != nil {
		req.RespondError(err)
		return
	}

	qid := dir2Qid(fid.st)
	req.RespondRattach(qid)
}

func (*VuFs) Flush(req *go9p.SrvReq) {}

func (*VuFs) Walk(req *go9p.SrvReq) {
	fid := req.Fid.Aux.(*vufsFid)
	tc := req.Tc

	err := fid.stat()
	if err != nil {
		req.RespondError(err)
		return
	}

	if req.Newfid.Aux == nil {
		req.Newfid.Aux = new(vufsFid)
	}

	nfid := req.Newfid.Aux.(*vufsFid)
	wqids := make([]go9p.Qid, len(tc.Wname))
	path := fid.path
	i := 0
	for ; i < len(tc.Wname); i++ {
		p := path + "/" + tc.Wname[i]
		st, err := os.Lstat(p)
		if err != nil {
			if i == 0 {
				req.RespondError(go9p.Enoent)
				return
			}

			break
		}

		wqids[i] = *dir2Qid(st)
		path = p
	}

	nfid.path = path
	req.RespondRwalk(wqids[0:i])
}

func (wfs *VuFs) Open(req *go9p.SrvReq) {
	fid := req.Fid.Aux.(*vufsFid)
	tc := req.Tc
	err := fid.stat()
	if err != nil {
		req.RespondError(err)
		return
	}

	var e error
	fid.file, e = os.OpenFile(fid.path, omode2uflags(tc.Mode), 0)
	if e != nil {
		req.RespondError(toError(e))
		return
	}

	req.RespondRopen(dir2Qid(fid.st), 0)
}

func (*VuFs) Create(req *go9p.SrvReq) {
	fid := req.Fid.Aux.(*vufsFid)
	tc := req.Tc
	err := fid.stat()
	if err != nil {
		req.RespondError(err)
		return
	}

	path := fid.path + "/" + tc.Name
	var e error = nil
	var file *os.File = nil
	switch {
	case tc.Perm&go9p.DMDIR != 0:
		e = os.Mkdir(path, os.FileMode(tc.Perm&0777))

	case tc.Perm&go9p.DMSYMLINK != 0:
		e = os.Symlink(tc.Ext, path)

	case tc.Perm&go9p.DMLINK != 0:
		n, e := strconv.ParseUint(tc.Ext, 10, 0)
		if e != nil {
			break
		}

		ofid := req.Conn.FidGet(uint32(n))
		if ofid == nil {
			req.RespondError(go9p.Eunknownfid)
			return
		}

		e = os.Link(ofid.Aux.(*vufsFid).path, path)
		ofid.DecRef()

	case tc.Perm&go9p.DMNAMEDPIPE != 0:
	case tc.Perm&go9p.DMDEVICE != 0:
		req.RespondError(&go9p.Error{"not implemented", go9p.EIO})
		return

	default:
		var mode uint32 = tc.Perm & 0777
		if req.Conn.Dotu {
			if tc.Perm&go9p.DMSETUID > 0 {
				mode |= syscall.S_ISUID
			}
			if tc.Perm&go9p.DMSETGID > 0 {
				mode |= syscall.S_ISGID
			}
		}
		file, e = os.OpenFile(path, omode2uflags(tc.Mode)|os.O_CREATE, os.FileMode(mode))
	}

	if file == nil && e == nil {
		file, e = os.OpenFile(path, omode2uflags(tc.Mode), 0)
	}

	if e != nil {
		req.RespondError(toError(e))
		return
	}

	fid.path = path
	fid.file = file
	err = fid.stat()
	if err != nil {
		req.RespondError(err)
		return
	}

	req.RespondRcreate(dir2Qid(fid.st), 0)
}

func (*VuFs) Read(req *go9p.SrvReq) {

	fid := req.Fid.Aux.(*vufsFid)
	tc := req.Tc
	rc := req.Rc
	err := fid.stat()
	if err != nil {
		req.RespondError(err)
		return
	}

	go9p.InitRread(rc, tc.Count)
	var count int
	var e error
	if fid.st.IsDir() {
		if tc.Offset == 0 {
			var e error
			// If we got here, it was open. Can't really seek
			// in most cases, just close and reopen it.
			fid.file.Close()
			if fid.file, e = os.OpenFile(fid.path, omode2uflags(req.Fid.Omode), 0); e != nil {
				req.RespondError(toError(e))
				return
			}

			if fid.dirs, e = fid.file.Readdir(-1); e != nil {
				req.RespondError(toError(e))
				return
			}

			fid.dirents = nil
			fid.direntends = nil
			for i := 0; i < len(fid.dirs); i++ {
				path := fid.path + "/" + fid.dirs[i].Name()
				st, _ := dir2Dir(path, fid.dirs[i], req.Conn.Srv.Upool)
				if st == nil {
					continue
				}
				b := go9p.PackDir(st, req.Conn.Dotu)
				fid.dirents = append(fid.dirents, b...)
				count += len(b)
				fid.direntends = append(fid.direntends, count)
			}
		}

		switch {
		case tc.Offset > uint64(len(fid.dirents)):
			count = 0
		case len(fid.dirents[tc.Offset:]) > int(tc.Count):
			count = int(tc.Count)
		default:
			count = len(fid.dirents[tc.Offset:])
		}

		copy(rc.Data, fid.dirents[tc.Offset:int(tc.Offset)+count])

	} else {
		count, e = fid.file.ReadAt(rc.Data, int64(tc.Offset))
		if e != nil && e != io.EOF {
			req.RespondError(toError(e))
			return
		}

	}

	go9p.SetRreadCount(rc, uint32(count))
	req.Respond()
}

func (*VuFs) Write(req *go9p.SrvReq) {
	fid := req.Fid.Aux.(*vufsFid)
	tc := req.Tc
	err := fid.stat()
	if err != nil {
		req.RespondError(err)
		return
	}

	n, e := fid.file.WriteAt(tc.Data, int64(tc.Offset))
	if e != nil {
		req.RespondError(toError(e))
		return
	}

	req.RespondRwrite(uint32(n))
}

func (*VuFs) Clunk(req *go9p.SrvReq) { req.RespondRclunk() }

func (*VuFs) Remove(req *go9p.SrvReq) {
	fid := req.Fid.Aux.(*vufsFid)
	err := fid.stat()
	if err != nil {
		req.RespondError(err)
		return
	}

	e := os.Remove(fid.path)
	if e != nil {
		req.RespondError(toError(e))
		return
	}

	req.RespondRremove()
}

func (*VuFs) Stat(req *go9p.SrvReq) {
	fid := req.Fid.Aux.(*vufsFid)
	err := fid.stat()
	if err != nil {
		req.RespondError(err)
		return
	}

	st, derr := dir2Dir(fid.path, fid.st, req.Conn.Srv.Upool)
	if st == nil {
		req.RespondError(derr)
		return
	}

	req.RespondRstat(st)
}

func lookup(uid string, group bool) (uint32, *go9p.Error) {
	if uid == "" {
		return go9p.NOUID, nil
	}
	usr, e := user.Lookup(uid)
	if e != nil {
		return go9p.NOUID, toError(e)
	}
	conv := usr.Uid
	if group {
		conv = usr.Gid
	}
	u, e := strconv.Atoi(conv)
	if e != nil {
		return go9p.NOUID, toError(e)
	}
	return uint32(u), nil
}

func (u *VuFs) Wstat(req *go9p.SrvReq) {
	fid := req.Fid.Aux.(*vufsFid)
	err := fid.stat()
	if err != nil {
		req.RespondError(err)
		return
	}

	dir := &req.Tc.Dir
	if dir.Mode != 0xFFFFFFFF {
		mode := dir.Mode & 0777
		e := os.Chmod(fid.path, os.FileMode(mode))
		if e != nil {
			req.RespondError(toError(e))
			return
		}
	}

	uid, gid := go9p.NOUID, go9p.NOUID

	// Try to find local uid, gid by name.
	if (dir.Uid != "" || dir.Gid != "")  {
		uid, err = lookup(dir.Uid, false)
		if err != nil {
			req.RespondError(err)
			return
		}

		// BUG(akumar): Lookup will never find gids
		// corresponding to group names, because
		// it only operates on user names.
		gid, err = lookup(dir.Gid, true)
		if err != nil {
			req.RespondError(err)
			return
		}
	}

	if uid != go9p.NOUID || gid != go9p.NOUID {
		e := os.Chown(fid.path, int(uid), int(gid))
		if e != nil {
			req.RespondError(toError(e))
			return
		}
	}

	if dir.Name != "" {
		fmt.Printf("Rename %s to %s\n", fid.path, dir.Name)
		// if first char is / it is relative to root, else relative to
		// cwd.
		var destpath string
		if dir.Name[0] == '/' {
			destpath = path.Join(u.Root, dir.Name)
			fmt.Printf("/ results in %s\n", destpath)
		} else {
			fiddir, _ := path.Split(fid.path)
			destpath = path.Join(fiddir, dir.Name)
			fmt.Printf("rel  results in %s\n", destpath)
		}
		err := syscall.Rename(fid.path, destpath)
		fmt.Printf("rename %s to %s gets %v\n", fid.path, destpath, err)
		if err != nil {
			req.RespondError(toError(err))
			return
		}
		fid.path = destpath
	}

	if dir.Length != 0xFFFFFFFFFFFFFFFF {
		e := os.Truncate(fid.path, int64(dir.Length))
		if e != nil {
			req.RespondError(toError(e))
			return
		}
	}

	// If either mtime or atime need to be changed, then
	// we must change both.
	if dir.Mtime != ^uint32(0) || dir.Atime != ^uint32(0) {
		mt, at := time.Unix(int64(dir.Mtime), 0), time.Unix(int64(dir.Atime), 0)
		if cmt, cat := (dir.Mtime == ^uint32(0)), (dir.Atime == ^uint32(0)); cmt || cat {
			st, e := os.Stat(fid.path)
			if e != nil {
				req.RespondError(toError(e))
				return
			}
			switch cmt {
			case true:
				mt = st.ModTime()
			default:
				//at = time.Time(0)//atime(st.Sys().(*syscall.Stat_t))
			}
		}
		e := os.Chtimes(fid.path, at, mt)
		if e != nil {
			req.RespondError(toError(e))
			return
		}
	}

	req.RespondRwstat()
}
