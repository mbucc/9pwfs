package vufs

import (
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/lionkov/go9p/p"
	"github.com/lionkov/go9p/p/srv"
)

const uidgidFile = ".uidgid"

type Fid struct {
	path string
	file *os.File
}

type VuFs struct {
	srv.Srv
	Root string
}

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

	return ret
}

func dir2Npmode(d os.FileInfo) uint32 {

	ret := uint32(d.Mode() & 0777)

	if d.IsDir() {
		ret |= p.DMDIR
	}

	return ret
}

// Convert a string user id to a string and look up user Name.
func uid2name(id string, upool p.Users) (string, error) {

	uid, err := strconv.Atoi(id)

	if err != nil {
		return "", fmt.Errorf("invalid uid '%s'", id)
	}

	u := upool.Uid2User(uid)

	if u == nil {
		return "", fmt.Errorf("no user with id %d", uid)
	}

	return u.Name(), nil

}

// Lookup (uid, gid) for a file (path = full path to file, e.g. './tmpfs/test.txt')
func path2UserGroup(path string, upool p.Users) (string, string, error) {

	// Default owner/group is adm.
	user := "adm"
	group := "adm"

	dn := filepath.Dir(path)
	fn := filepath.Base(path)

	data, err := ioutil.ReadFile(filepath.Join(dn, uidgidFile))
	if err != nil {
		if os.IsNotExist(err) {
			return user, group, nil
		} else {
			return "", "", err
		}
	}

	sdata := string(data)

	for _, line := range strings.Split(sdata, "\n") {

		if line == "#" {
			continue
		}

		columns := strings.Split(line, ":")
		if len(columns) != 3 {
			continue
		}

		if columns[0] == fn {

			user, err = uid2name(columns[1], upool)

			if err != nil {
				return "", "", err
			}

			group, err = uid2name(columns[2], upool)

			if err != nil {
				return "", "", err
			}

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

	dir := new(p.Dir)
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

	return dir, nil
}

func mode2Perm(mode uint8) uint32 {
	var perm uint32 = 0

	switch mode & 3 {
	case p.OREAD:
		perm = p.DMREAD
	case p.OWRITE:
		perm = p.DMWRITE
	case p.ORDWR:
		perm = p.DMREAD | p.DMWRITE
	}

	if (mode & p.OTRUNC) != 0 {
		perm |= p.DMWRITE
	}

	return perm
}

// Checks if the specified user has permission to perform
// certain operation on a file. Perm contains one or more
// of DMREAD, DMWRITE, and DMEXEC.
func CheckPerm(f *p.Dir, user p.User, perm uint32) bool {

	if user == nil {
		return false
	}

	perm &= 7

	/* other permissions */
	fperm := f.Mode & 7
	if (fperm & perm) == perm {

		return true
	}

	/* user permissions */
	if f.Uid == user.Name() || f.Uidnum == uint32(user.Id()) {
		fperm |= (f.Mode >> 6) & 7
	}

	if (fperm & perm) == perm {

		return true
	}

	/* group permissions */
	groups := user.Groups()
	if groups != nil && len(groups) > 0 {
		for i := 0; i < len(groups); i++ {
			if f.Gid == groups[i].Name() || f.Gidnum == uint32(groups[i].Id()) {
				fperm |= (f.Mode >> 3) & 7
				break
			}
		}
	}

	if (fperm & perm) == perm {

		return true
	}

	return false
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
	if fid != nil {
		fid.file.Close()
	}
}

// Always attach to the VuFs root.
func (u *VuFs) Attach(req *srv.Req) {

	if req.Tc.Aname != "/" && req.Tc.Aname != "" {
		req.RespondError(srv.Eperm)
		return
	}

	st, err := os.Stat(u.Root)
	if err != nil {
		req.RespondError(toError(err))
		return
	}

	fid := new(Fid)
	fid.path = u.Root
	req.Fid.Aux = fid

	qid := dir2Qid(st)
	req.RespondRattach(qid)
}

func (*VuFs) Flush(req *srv.Req) {}

// BUG(mbucc) does not fully implement spec when fid = newfid.
// From http://plan9.bell-labs.com/magic/man2html/5/walk:
//	If newfid is the same as fid, the above discussion applies, with the
//	obvious difference that if the walk changes the state of newfid, it
//	also changes the state of fid; and if newfid is unaffected, then fid
//	is also unaffected.
//
func (u *VuFs) Walk(req *srv.Req) {
	fid := req.Fid.Aux.(*Fid)
	tc := req.Tc

	_, err := os.Stat(fid.path)
	if err != nil {
		req.RespondError(toError(err))
		return
	}

	if req.Newfid.Aux == nil {
		req.Newfid.Aux = new(Fid)
	}

	newfid := req.Newfid.Aux.(*Fid)
	wqids := make([]p.Qid, len(tc.Wname))
	path := fid.path
	i := 0

	// Ensure execute permission on the walk root.
	st, err := os.Stat(path)
	if err != nil {
		req.RespondError(srv.Enoent)
		return
	}
	f, err := dir2Dir(path, st, req.Conn.Srv.Upool)
	if err != nil {
		req.RespondError(toError(err))
		return
	}
	if !CheckPerm(f, req.Fid.User, p.DMEXEC) {
		req.RespondError(srv.Eperm)
		return
	}

	for ; i < len(tc.Wname); i++ {

		var newpath string

		// Don't allow client to dotdot out of the file system root.
		if tc.Wname[i] == ".." {
			if path == u.Root {
				continue
			} else {
				newpath = path[:strings.LastIndex(path, "/")]
				if newpath == u.Root {
					continue
				}
			}
		} else {
			newpath = path + "/" + tc.Wname[i]
		}

		st, err := os.Stat(newpath)
		if err != nil {
			if i == 0 {
				req.RespondError(srv.Enoent)
				return
			}

			break
		}

		wqids[i] = *dir2Qid(st)

		if (wqids[i].Type & p.QTDIR) > 0 {
			f, err := dir2Dir(newpath, st, req.Conn.Srv.Upool)
			if err != nil {
				req.RespondError(toError(err))
				return
			}
			if !CheckPerm(f, req.Fid.User, p.DMEXEC) {
				req.RespondError(srv.Eperm)
				return
			}
		}

		path = newpath
	}

	newfid.path = path
	req.RespondRwalk(wqids[0:i])
}

func (u *VuFs) Open(req *srv.Req) {
	fid := req.Fid.Aux.(*Fid)
	tc := req.Tc

	// Ensure open permission.
	st, err := os.Stat(fid.path)
	if err != nil {
		req.RespondError(srv.Enoent)
		return
	}
	f, err := dir2Dir(fid.path, st, req.Conn.Srv.Upool)
	if err != nil {
		req.RespondError(toError(err))
		return
	}
	if !CheckPerm(f, req.Fid.User, mode2Perm(tc.Mode)) {
		req.RespondError(srv.Eperm)
		return
	}

	var e error
	fid.file, e = os.OpenFile(fid.path, omode2uflags(tc.Mode), 0)
	if e != nil {
		req.RespondError(toError(e))
		return
	}

	req.RespondRopen(dir2Qid(st), 0)
}

func addUidGid(dir, file string, uid, gid int, fid *srv.Fid) error {

	fid.Lock()
	defer fid.Unlock()

	fn0 := dir + "/" + uidgidFile
	//fn1 := fn0 + ".tmp"

	fp0, err := os.OpenFile(fn0, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		return err
	}

	defer fp0.Close()

	_, err = fp0.WriteString(fmt.Sprintf("%s:%d:%d\n", file, uid, gid))
	if err != nil {
		// BUG(mbucc) Roll back  bytes written to .uidgid on error.
		return err
	}

/*

	fp0, err := os.OpenFile(fn0, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err == nil {
		defer fp0.Close()
		_, err = fp0.WriteString(fmt.Sprintf("%s:%s:%s\n", file, uid, uid))

	switch err {
	case nil:

	if err == nil && os.IsNotExist(err){
		return err
	}

	if err != nil {




*/

	return nil
}


func (*VuFs) Create(req *srv.Req) {
	fid := req.Fid.Aux.(*Fid)
	tc := req.Tc

	parentPath := fid.path

	// User must be able to write to parent directory.
	st, err := os.Stat(parentPath)
	if err != nil {
		req.RespondError(toError(err))
		return
	}
	f, err := dir2Dir(parentPath, st, req.Conn.Srv.Upool)
	if err != nil {
		req.RespondError(toError(err))
		return
	}
	if !CheckPerm(f, req.Fid.User, p.DMWRITE) {
		req.RespondError(srv.Eperm)
		return
	}

	path := parentPath + "/" + tc.Name
	var e error = nil
	var file *os.File = nil
	switch {
	case tc.Perm&p.DMDIR != 0:
		e = os.Mkdir(path, os.FileMode(tc.Perm&0777))
		if e == nil {
			file, e = os.OpenFile(path, omode2uflags(tc.Mode), 0)
		}

	case tc.Perm&p.DMSYMLINK != 0,
			tc.Perm&p.DMLINK != 0,
			tc.Perm&p.DMNAMEDPIPE != 0,
			tc.Perm&p.DMDEVICE != 0,
			tc.Perm&p.DMSOCKET != 0,
			tc.Perm&p.DMSETUID != 0,
			tc.Perm&p.DMSETGID != 0:
		req.RespondError(srv.Ebaduse)
		return

	default:
		var mode uint32 = tc.Perm & 0777
		file, e = os.OpenFile(path,
			omode2uflags(tc.Mode)|os.O_CREATE,
			os.FileMode(mode))
	}

	if e != nil {
		req.RespondError(toError(e))
		return
	}

	fid.path = path
	fid.file = file
	st, err = os.Stat(fid.path)
	if err != nil {
		file.Close()
		fid.file = nil
		req.RespondError(err)
		return
	}

	// BUG(mbucc): Redesign data structures so I can remove this panic.
	_, dirgid, err := path2UserGroup(parentPath, req.Conn.Srv.Upool)
	if err != nil {
		panic(fmt.Sprintf("no uid/gid found for parent directory '%s'", parentPath))
	}
	gu := req.Conn.Srv.Upool.Uname2User(dirgid)
	if gu == nil {
		panic(fmt.Sprintf("no user for parent directory gid %d", dirgid))
	}
	
	err = addUidGid(parentPath, tc.Name, req.Fid.User.Id(), gu.Id(), req.Fid)
	if err != nil {
		file.Close()
		fid.file = nil
		req.RespondError(err)
		return
	}

	req.RespondRcreate(dir2Qid(st), 0)
}

func (u *VuFs) Read(req *srv.Req) {
	fid := req.Fid.Aux.(*Fid)
	tc := req.Tc
	rc := req.Rc
	st, err := os.Stat(fid.path)
	if err != nil {
		req.RespondError(err)
		return
	}

	p.InitRread(rc, tc.Count)
	var count int
	var e error
	if st.IsDir() {
		// Simpler to treat non-zero offset as an error for directories.
		if tc.Offset != 0 {
			req.RespondError(srv.Ebadoffset)
			return
		}

		dirs, e := fid.file.Readdir(-1)

		if e != nil {
			req.RespondError(toError(e))
			return
		}

		// Bytes/one packed dir = 49 + len(name) + len(uid) + len(gid) + len(muid)
		// Estimate 49 + 20 + 20 + 20 + 11
		// From ../../lionkov/go9p/p/p9.go:421,427
		dirents := make([]byte, 0, 120 * len(dirs))
		for i := 0; i < len(dirs); i++ {
			path := fid.path + "/" + dirs[i].Name()
			st, err := dir2Dir(path, dirs[i], req.Conn.Srv.Upool)
			if err != nil {
				req.RespondError(toError(err))
				return
			}
			b := p.PackDir(st, false)
			dirents = append(dirents, b...)
		}

		if len(dirents) > int(tc.Count) {
			req.RespondError(srv.Etoolarge)
			return
		}

		copy(rc.Data, dirents)

		count = len(dirents)

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
	_, err := os.Stat(fid.path)
	if err != nil {
		req.RespondError(toError(err))
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
	_, err := os.Stat(fid.path)
	if err != nil {
		req.RespondError(toError(err))
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
	st, err := os.Stat(fid.path)

	if err != nil {
		req.RespondError(toError(err))
		return
	}

	dir, err := dir2Dir(fid.path, st, req.Conn.Srv.Upool)
	if err != nil {
		req.RespondError(err)
		return
	}
	req.RespondRstat(dir)
}

func (u *VuFs) Wstat(req *srv.Req) {
	fid := req.Fid.Aux.(*Fid)
	_, err := os.Stat(fid.path)
	if err != nil {
		req.RespondError(toError(err))
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

/*
	// BUG(mbucc) implement chown
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
*/
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
			newname = path.Join(fid.path, dir.Name)
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

func New(root string) *VuFs {
	return &VuFs{Root: root}
}
