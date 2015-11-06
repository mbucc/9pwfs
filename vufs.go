package vufs

import (
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/lionkov/go9p/p"
	"github.com/lionkov/go9p/p/srv"
)


type Fid struct {
	path string
	file *os.File
}

type Conn struct {
	rwc   io.ReadWriteCloser
	srv   *VuFs
	dying bool
	fids  map[uint32]*File
}

type ConnFcall struct {
	conn *Conn
	fc   *Fcall
	emsg string
}

type File struct {
	info *Dir
	list []File
}

type Tree struct {
	root *File
}

type VuFs struct {
	sync.Mutex
	Root          string
	dying         bool
	connections   []*Conn
	connchan      chan net.Conn
	fcallchan     chan *ConnFcall
	chatty        bool
	connchanDone  chan bool
	fcallchanDone chan bool
	listener      net.Listener
	tree          *Tree
}

func (vu *VuFs) Chatty(b bool) {
	vu.chatty = b
}

func (vu *VuFs) chat(msg string) {
	if vu.chatty {
		fmt.Println("vufs: " + msg)
	}
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

	data, err := ioutil.ReadFile(filepath.Join(dn, "uidgidFile"))
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

func chown(dir, file string, uid, gid int, fid *srv.Fid) error {

	fid.Lock()
	defer fid.Unlock()

	fn0 := dir + "/" + "uidgidFile"
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

func addUidGid(dir, file string, uid, gid int) error {

	fn0 := dir + "/" + "uidgidFile"
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

	err = addUidGid(parentPath, tc.Name, req.Fid.User.Id(), gu.Id())
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
		dirents := make([]byte, 0, 120*len(dirs))
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

		if dir.Uid != "" || dir.Gid != "" {
			uid0, gid0, err := path2UserGroup(fid.path, req.Conn.Srv.Upool)
			if err != nil {
				panic("can't get user/group for " + fid.path + ": " + err.Error())
			}

			uid1, gid1 := uid0, gid0
			if dir.Uid != "" {
				uid1 = dir.Uid
			}
			if dir.Gid != "" {
				gid1 := dir.Gid
			}

			err = os.Chown(fid.path, int(uid1), int(gid1))
			if err != nil {
				panic("can't set user/group for " + fid.path + ": " + err.Error())
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

// Respond with an error.
func (vu *VuFs) rerror(r *ConnFcall) {
	rc := &Fcall{Type: Rerror, Msize: r.fc.Msize, Ename: r.emsg}
	vu.chat("-> " + rc.String())
	WriteFcall(r.conn.rwc, rc)
}

// Respond to Version message.
func (vu *VuFs) rversion(r *ConnFcall) {
	vu.chat("<- " + r.fc.String())

	// We only support 9P2000.
	ver := r.fc.Version
	i := strings.Index(ver, ".")
	if i > 0 {
		ver = ver[:i]
	}
	if ver != VERSION9P {
		ver = "unknown"
	}

	// Clamp message size.
	msz := r.fc.Msize
	if msz > MAX_MSIZE {
		msz = MAX_MSIZE
	}

	// Drain any pending fcalls.
	done := false
	for ver != "unknown" && !done {
		select {
		case x := <-vu.fcallchan:
			x.emsg = "new session started, dropping this pending request"
			vu.rerror(x)
		default:
			done = true
		}
	}

	rc := &Fcall{Type: Rversion, Msize: msz, Version: ver}
	vu.chat("-> " + rc.String())
	WriteFcall(r.conn.rwc, rc)
}

// Respond to Attach message.
func (vu *VuFs) rattach(r *ConnFcall) {
	vu.chat("<- " + r.fc.String())

	// To simplify dot-dot logic in walk, we  only allow attaches to root.
	if r.fc.Aname != "/" {
		r.emsg = "can only attach to root directory"
		vu.rerror(r)
	}

	// We don't support authentication.
	if r.fc.Afid != NOFID {
		r.emsg = "authentication not supported"
		vu.rerror(r)
	}

	if _, inuse := r.conn.fids[r.fc.Fid]; inuse {
		r.emsg = "fid already in use on this connection"
		vu.rerror(r)
	}
	r.conn.fids[r.fc.Fid] = vu.tree.root

	rc := &Fcall{Type: Rattach, Qid: vu.tree.root.info.Qid}
	vu.chat("-> " + rc.String())
	WriteFcall(r.conn.rwc, rc)
}

// Response to Auth message.
func (vu *VuFs) rauth(r *ConnFcall) {
	vu.chat("<- " + r.fc.String())
	r.emsg = "not supported"
	vu.rerror(r)
}

// Response to Stat message.
func (vu *VuFs) rstat(r *ConnFcall) {
	var err error

	vu.chat("<- " + r.fc.String())
	if file, found := r.conn.fids[r.fc.Fid]; found {
		rc := &Fcall{Type: Rstat}
		rc.Stat, err = file.info.Bytes()
		if err != nil {
			r.emsg = "stat: " + err.Error()
			vu.rerror(r)
		} else {
			WriteFcall(r.conn.rwc, rc)
		}
	} else {
		r.emsg = "fid not found"
		vu.rerror(r)
	}
}

// Read file system calls off channel one-by-one.
func (vu *VuFs) fcallhandler() {
	for {
		x, more := <-vu.fcallchan
		if more {
			if f, ok := fcallhandlers[x.fc.Type]; ok {
				f(x)
			} else {
				vu.chat(string(x.fc.Type) + "was not found")
				x.emsg = "not implemented"
				vu.rerror(x)
			}
		} else {
			vu.chat("fcallchan closed")
			vu.fcallchanDone <- true
			return
		}
	}
}

// Read file system call from connection and push (serialize)
// onto our one file system call channel.
func (c *Conn) recv() {
	for !c.dying {
		fc, err := ReadFcall(c.rwc)
		if err == nil {
			c.srv.fcallchan <- &ConnFcall{c, fc, ""}
		} else {
			if !c.dying {
				c.srv.chat("recv() error: " + err.Error())
			}
			continue
		}
	}
	c.srv.chat("recv() done")
}

// Add connection to connection list and spawn a go routine
// to process messages received on the new connection.
func (vu *VuFs) connhandler() {
	for {
		vu.chat("connhandler")
		conn, more := <-vu.connchan
		if more {
			c := &Conn{rwc: conn, srv: vu, fids: make(map[uint32]*File)}
			vu.connections = append(vu.connections, c)
			go c.recv()
		} else {
			vu.chat("connchan closed")
			return
		}
	}
}

// Serialize connection requests by fanning-in to one channel.
func (vu *VuFs) listen() error {
	var err error
	vu.chat("start listening for connections")
	for {
		c, err := vu.listener.Accept()
		if err != nil {
			break
		}
		vu.chat("new connection")
		vu.connchan <- c
	}
	if err != nil {
		vu.chat("error!")
	}
	vu.chat("stop listening for connections")
	vu.connchanDone <- true
	return nil
}

// Start listening for connections.
func (vu *VuFs) Start(ntype, addr string) error {
	vu.Lock()
	defer vu.Unlock()

	vu.chat("start")

	err := vu.buildtree()
	if err != nil {
		return err
	}

	vu.listener, err = net.Listen(ntype, addr)
	if err != nil {
		return err
	}
	go vu.connhandler()
	go vu.listen()
	go vu.fcallhandler()
	return nil
}

// Stop listening, drain channels, wait any in-progress work to finish, and shut down.
func (vu *VuFs) Stop() {
	vu.Lock()
	defer vu.Unlock()

	vu.dying = true
	close(vu.connchan)

	close(vu.fcallchan)
	for x := range vu.fcallchan {
		x.emsg = "file system stopped"
		vu.rerror(x)
	}

	for _, c := range vu.connections {
		c.dying = true
		c.rwc.Close()
	}
	vu.listener.Close()
	<-vu.connchanDone
	<- vu.fcallchanDone
}

func (f *File) init(rootdir string) error {

	info, err := os.Stat(rootdir)
	if err != nil {
		return err
	}

	sysif := info.Sys()
	if sysif == nil {
		return fmt.Errorf("no stat datasource for '%s'", rootdir)
	}
	var sysMode *syscall.Stat_t
	switch t := sysif.(type) {
	case *syscall.Stat_t:
		sysMode = t
	default:
		return fmt.Errorf("stat datasource is not a Stat_t for '%s'", rootdir)
	}
	stat := sysif.(*syscall.Stat_t)

	dir := new(Dir)
	dir.Null()

	dir.Qid.Path = stat.Ino
	dir.Qid.Vers = uint32(info.ModTime().UnixNano() / 1000000)
	dir.Mode = Perm(info.Mode() & 0777)

	dir.Atime = uint32(atime(sysMode).Unix())
	dir.Mtime = uint32(info.ModTime().Unix())
	dir.Length = uint64(info.Size())
	dir.Name = "/" // rootdir[strings.LastIndex(rootdir, "/")+1:]

	if info.IsDir() {
		dir.Mode |= p.DMDIR
		dir.Qid.Vers |= p.QTDIR
		dir.Length = 0
	}

	dir.Uid = DEFAULT_USER
	dir.Gid = DEFAULT_USER
	dir.Muid = DEFAULT_USER


	f.info = dir

	return nil
}

func (vu *VuFs) buildtree() error {
	f := new(File)
	err := f.init(vu.Root)
	if err != nil {
		return err
	}
	vu.tree = &Tree{root: f}
	return nil
}

var fcallhandlers map[uint8]func(*ConnFcall)

func New(root string) *VuFs {

	vu := new(VuFs)
	vu.Root = root
	vu.connchan = make(chan net.Conn)
	vu.fcallchan = make(chan *ConnFcall)
	vu.connchanDone = make(chan bool)
	vu.fcallchanDone = make(chan bool)

	fcallhandlers = map[uint8]func(*ConnFcall){
		Tversion: vu.rversion,
		Tattach:  vu.rattach,
		Tauth:    vu.rauth,
		Tstat:    vu.rstat,
	}

	return vu
}
