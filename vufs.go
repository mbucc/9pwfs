package vufs

import (
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/user"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/lionkov/go9p/p"
	"github.com/lionkov/go9p/p/srv"
)

const uidgidFile = ".uidgid"

type Fid struct {
	path       string
	file       *os.File
	dirs       []os.FileInfo
	diroffset  uint64
	direntends []int
	dirents    []byte
	st         os.FileInfo
}

type VuFs struct {
	srv.Srv
	Root string
}

var root = flag.String("root", "/", "root filesystem")
var Enoent = &p.Error{"file not found", p.ENOENT}

func toError(err error) *p.Error {
	var ecode uint32

	ename := err.Error()
	if e, ok := err.(syscall.Errno); ok {
		ecode = uint32(e)
	} else {
		ecode = p.EIO
	}

	return &p.Error{ename, ecode}
}

// IsBlock reports if the file is a block device
func isBlock(d os.FileInfo) bool {
	sysif := d.Sys()
	if sysif == nil {
		return false
	}
	stat := sysif.(*syscall.Stat_t)
	return (stat.Mode & syscall.S_IFMT) == syscall.S_IFBLK
}

// IsChar reports if the file is a character device
func isChar(d os.FileInfo) bool {
	sysif := d.Sys()
	if sysif == nil {
		return false
	}
	stat := sysif.(*syscall.Stat_t)
	return (stat.Mode & syscall.S_IFMT) == syscall.S_IFCHR
}

func (fid *Fid) stat() *p.Error {
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
	case p.OREAD:
		ret = os.O_RDONLY
		break

	case p.ORDWR:
		ret = os.O_RDWR
		break

	case p.OWRITE:
		ret = os.O_WRONLY
		break

	case p.OEXEC:
		ret = os.O_RDONLY
		break
	}

	if mode&p.OTRUNC != 0 {
		ret |= os.O_TRUNC
	}

	return ret
}

func dir2Qid(d os.FileInfo) *p.Qid {
	var qid p.Qid
	sysif := d.Sys()
	if sysif == nil {
		return nil
	}
	stat := sysif.(*syscall.Stat_t)

	qid.Path = stat.Ino
	qid.Version = uint32(d.ModTime().UnixNano() / 1000000)
	qid.Type = dir2QidType(d)

	return &qid
}

func dir2QidType(d os.FileInfo) uint8 {
	ret := uint8(0)
	if d.IsDir() {
		ret |= p.QTDIR
	}

	if d.Mode()&os.ModeSymlink != 0 {
		ret |= p.QTSYMLINK
	}

	return ret
}

func dir2Npmode(d os.FileInfo) uint32 {

	ret := uint32(d.Mode() & 0777)

	if d.IsDir() {
		ret |= p.DMDIR
	}

	return ret
}

// Dir is an instantiation of the p.Dir structure
// that can act as a receiver for local methods.
type Dir struct {
	p.Dir
}

// Lookup (uid, gid) in uidgidFile and return (user,group).  If not found return (adm, adm).
func path2UserGroup(path string, upool p.Users) (string, string, error) {

	// Default owner/group is adm.
	user := "adm"
	group := "adm"

	fn := filepath.Join(filepath.Dir(path), uidgidFile)
	data, err := ioutil.ReadFile(fn)
	if err != nil {
		if os.IsNotExist(err) {
			return user, group, nil
		} else {
			return "", "", err
		}
	}

	sdata := string(data)
	for lineno, line := range strings.Split(sdata, "\n") {
		columns := strings.Split(line, ":")
		if len(columns) != 3 {
			continue
		}
		if columns[0] == path {
			uid, err := strconv.Atoi(columns[1])
			if err != nil {
				return "", "", fmt.Errorf("Atoi(uid) failed %s:%d -- %v", fn, lineno+1, err)
			}
			u := upool.Uid2User(uid)
			if u == nil {
				return "", "",
					fmt.Errorf("No user found for uid %d of %s, %s:%d", uid, path, fn, lineno+1)
			}
			user = u.Name()

			gid, err := strconv.Atoi(columns[2])
			if err != nil {
				return "", "", fmt.Errorf("Atoi(gid) failed  %s:%d -- %v", fn, lineno+1, err)
			}
			u = upool.Uid2User(gid)
			if u == nil {
				return "", "",
					fmt.Errorf("No user found for gid %d of %s, %s:%d", gid, path, fn, lineno+1)
			}
			group = u.Name()

			break
		}
	}

	return user, group, nil
}


func dir2Dir(s string, d os.FileInfo, upool p.Users) (*p.Dir, error) {
	sysif := d.Sys()
	if sysif == nil {
		return nil, &os.PathError{"dir2Dir", s, nil}
	}
	var sysMode *syscall.Stat_t
	switch t := sysif.(type) {
	case *syscall.Stat_t:
		sysMode = t
	default:
		return nil, &os.PathError{"dir2Dir: sysif has wrong type", s, nil}
	}

	dir := new(Dir)
	dir.Qid = *dir2Qid(d)
	dir.Mode = dir2Npmode(d)
	dir.Atime = uint32(atime(sysMode).Unix())
	dir.Mtime = uint32(d.ModTime().Unix())
	dir.Length = uint64(d.Size())
	dir.Name = s[strings.LastIndex(s, "/")+1:]

	uid, gid, err := path2UserGroup(s, upool)
	if err != nil {
		return nil, err
	}
	dir.Uid, dir.Gid = uid, gid
 

	return &dir.Dir, nil
}


func (*VuFs) ConnOpened(conn *srv.Conn) {
	if conn.Srv.Debuglevel > 0 {
		log.Println("connected")
	}
}

func (*VuFs) ConnClosed(conn *srv.Conn) {
	if conn.Srv.Debuglevel > 0 {
		log.Println("disconnected")
	}
}

func (*VuFs) FidDestroy(sfid *srv.Fid) {
	var fid *Fid

	if sfid.Aux == nil {
		return
	}

	fid = sfid.Aux.(*Fid)
	if fid.file != nil {
		fid.file.Close()
	}
}

func (u *VuFs) Attach(req *srv.Req) {
	if req.Afid != nil {
		req.RespondError(srv.Enoauth)
		return
	}

	tc := req.Tc
	fid := new(Fid)

	// You can think of the ufs.Root as a 'chroot' of a sort.
	// client attaches are not allowed to go outside the
	// directory represented by ufs.Root
	fid.path = path.Join(u.Root, tc.Aname)

	req.Fid.Aux = fid
	err := fid.stat()
	if err != nil {
		req.RespondError(err)
		return
	}

	qid := dir2Qid(fid.st)
	req.RespondRattach(qid)
}

func (*VuFs) Flush(req *srv.Req) {}

func (*VuFs) Walk(req *srv.Req) {
	fid := req.Fid.Aux.(*Fid)
	tc := req.Tc

	err := fid.stat()
	if err != nil {
		req.RespondError(err)
		return
	}

	if req.Newfid.Aux == nil {
		req.Newfid.Aux = new(Fid)
	}

	nfid := req.Newfid.Aux.(*Fid)
	wqids := make([]p.Qid, len(tc.Wname))
	path := fid.path
	i := 0
	for ; i < len(tc.Wname); i++ {
		p := path + "/" + tc.Wname[i]
		st, err := os.Lstat(p)
		if err != nil {
			if i == 0 {
				req.RespondError(Enoent)
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

func (*VuFs) Open(req *srv.Req) {
	fid := req.Fid.Aux.(*Fid)
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

func (*VuFs) Create(req *srv.Req) {
	fid := req.Fid.Aux.(*Fid)
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
	case tc.Perm&p.DMDIR != 0:
		e = os.Mkdir(path, os.FileMode(tc.Perm&0777))

	case tc.Perm&p.DMSYMLINK != 0:
		e = os.Symlink(tc.Ext, path)

	case tc.Perm&p.DMLINK != 0:
		n, e := strconv.ParseUint(tc.Ext, 10, 0)
		if e != nil {
			break
		}

		ofid := req.Conn.FidGet(uint32(n))
		if ofid == nil {
			req.RespondError(srv.Eunknownfid)
			return
		}

		e = os.Link(ofid.Aux.(*Fid).path, path)
		ofid.DecRef()

	case tc.Perm&p.DMNAMEDPIPE != 0:
	case tc.Perm&p.DMDEVICE != 0:
		req.RespondError(&p.Error{"not implemented", p.EIO})
		return

	default:
		var mode uint32 = tc.Perm & 0777
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

func (u *VuFs) Read(req *srv.Req) {
	dbg := u.Debuglevel&srv.DbgLogFcalls != 0
	fid := req.Fid.Aux.(*Fid)
	tc := req.Tc
	rc := req.Rc
	err := fid.stat()
	if err != nil {
		req.RespondError(err)
		return
	}

	p.InitRread(rc, tc.Count)
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

			if dbg {
				log.Printf("Read: read %d entries", len(fid.dirs))
			}
			fid.dirents = nil
			fid.direntends = nil
			for i := 0; i < len(fid.dirs); i++ {
				path := fid.path + "/" + fid.dirs[i].Name()
				st, err := dir2Dir(path, fid.dirs[i], req.Conn.Srv.Upool)
				if err != nil {
					if dbg {
						log.Printf("dbg: stat of %v: %v", path, err)
					}
					continue
				}
				if dbg {
					log.Printf("Stat: %v is %v", path, st)
				}
				b := p.PackDir(st, false)
				fid.dirents = append(fid.dirents, b...)
				count += len(b)
				fid.direntends = append(fid.direntends, count)
				if dbg {
					log.Printf("fid.direntends is %v\n", fid.direntends)
				}
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

		if dbg {
			log.Printf("readdir: count %v @ offset %v", count, tc.Offset)
		}
		nextend := sort.SearchInts(fid.direntends, int(tc.Offset)+count)
		if nextend < len(fid.direntends) {
			if fid.direntends[nextend] > int(tc.Offset)+count {
				if nextend > 0 {
					count = fid.direntends[nextend-1] - int(tc.Offset)
				} else {
					count = 0
				}
			}
		}
		if dbg {
			log.Printf("readdir: count adjusted %v @ offset %v", count, tc.Offset)
		}
		if count == 0 && int(tc.Offset) < len(fid.dirents) && len(fid.dirents) > 0 {
			req.RespondError(&p.Error{"too small read size for dir entry", p.EINVAL})
			return
		}
		copy(rc.Data, fid.dirents[tc.Offset:int(tc.Offset)+count])
	} else {
		count, e = fid.file.ReadAt(rc.Data, int64(tc.Offset))
		if e != nil && e != io.EOF {
			req.RespondError(toError(e))
			return
		}
	}

	p.SetRreadCount(rc, uint32(count))
	req.Respond()
}

func (*VuFs) Write(req *srv.Req) {
	fid := req.Fid.Aux.(*Fid)
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

func (*VuFs) Clunk(req *srv.Req) { req.RespondRclunk() }

func (*VuFs) Remove(req *srv.Req) {
	fid := req.Fid.Aux.(*Fid)
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

func (*VuFs) Stat(req *srv.Req) {
	fid := req.Fid.Aux.(*Fid)
	if err := fid.stat(); err != nil {
		req.RespondError(err)
		return
	}

	st, err := dir2Dir(fid.path, fid.st, req.Conn.Srv.Upool)
	if err != nil {
		req.RespondError(err)
		return
	}
	req.RespondRstat(st)
}

func lookup(uid string, group bool) (uint32, *p.Error) {
	if uid == "" {
		return p.NOUID, nil
	}
	usr, e := user.Lookup(uid)
	if e != nil {
		return p.NOUID, toError(e)
	}
	conv := usr.Uid
	if group {
		conv = usr.Gid
	}
	u, e := strconv.Atoi(conv)
	if e != nil {
		return p.NOUID, toError(e)
	}
	return uint32(u), nil
}

func (u *VuFs) Wstat(req *srv.Req) {
	fid := req.Fid.Aux.(*Fid)
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

	uid, gid := p.NOUID, p.NOUID

	uid, err = lookup(dir.Uid, false)
	if err != nil {
		req.RespondError(err)
		return
	}

	gid, err = lookup(dir.Gid, true)
	if err != nil {
		req.RespondError(err)
		return
	}

	if uid != p.NOUID || gid != p.NOUID {
		e := os.Chown(fid.path, int(uid), int(gid))
		if e != nil {
			req.RespondError(toError(e))
			return
		}
	}

	if dir.Name != "" {
		// If we path.Join dir.Name to / before adding it to
		// the fid path, that ensures nobody gets to walk out of the
		// root of this server.
		newname := path.Join(path.Dir(fid.path), path.Join("/", dir.Name))

		// absolute renaming. VuFs can do this, so let's support it.
		// We'll allow an absolute path in the Name and, if it is,
		// we will make it relative to root. This is a gigantic performance
		// improvement in systems that allow it.
		if filepath.IsAbs(dir.Name) {
			newname = path.Join(u.Root, dir.Name)
		}

		err := syscall.Rename(fid.path, newname)
		if err != nil {
			req.RespondError(toError(err))
			return
		}
		fid.path = newname
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
				at = atime(st.Sys().(*syscall.Stat_t))
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

/*
func New() *VuFs {
	return &VuFs{Root: *root}
}
*/
