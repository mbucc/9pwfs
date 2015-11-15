package vufs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Golang Flags (not all may be implemented by underlying operating system):
// An "x" means it is handled by this routine.
//		    x    O_RDONLY
//		    x    O_WRONLY
//		    x    O_RDWR
//		    x    O_APPEND
//		          O_CREATE    - set manually in File.Create
//		    x    O_EXCL
//		          O_SYNC
//		    x    O_TRUNC
func openflags(mode uint8, perm Perm) int {
	ret := int(0)
	switch mode & 3 {
	case OREAD:
		ret = os.O_RDONLY
		break
	case ORDWR:
		ret = os.O_RDWR
		break
	case OWRITE:
		ret = os.O_WRONLY
		break
	case OEXEC:
		ret = os.O_RDONLY
		break
	}
	if mode&OTRUNC != 0 {
		ret |= os.O_TRUNC
	}
	if perm&DMAPPEND != 0 {
		ret |= os.O_APPEND
	}
	if perm&DMEXCL != 0 {
		ret |= os.O_EXCL
	}

	return ret
}

// NewFile creates a new File and then opens it.

func writeOwnership(path, uid, gid string) error {
	fn := path + ".vufs"
	fp, err := os.OpenFile(fn, os.O_TRUNC|os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		return err
	}
	defer fp.Close()

	_, err = fp.WriteString(fmt.Sprintf("%s:%s\n", uid, gid))
	if err != nil {
		return err
	}

	return nil
}

// Since we serialize all file operations, we can reuse the same response memory.
var rc *Fcall = new(Fcall)

// Respond to Version message.
func (vu *VuFs) rversion(r *ConnFcall) string {

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

	// A version message resets the session, which means
	// we drain any pending fcalls.
	done := false
	for ver != "unknown" && !done {
		select {
		case <-vu.fcallchan:
			return "new session started, dropping this request"
		default:
			done = true
		}
	}

	r.conn.msize = msz

	rc.Type = Rversion
	rc.Msize = msz
	rc.Version = ver
	return ""
}

// Respond to Attach message.
func (vu *VuFs) rattach(r *ConnFcall) string {

	// To simplify things, we only allow an attach to root of file server.
	if r.fc.Aname != "/" {
		return "can only attach to root directory"
	}

	// We don't support authentication.
	if r.fc.Afid != NOFID {
		return "authentication not supported"
	}

	if _, inuse := r.conn.fids[r.fc.Fid]; inuse {
		return "fid already in use on this connection"
	}

	r.conn.fids[r.fc.Fid] = &Fid{vu.tree.root, r.fc.Uname, false}
	rc.Qid = vu.tree.root.Qid
	return ""
}

// Response to Auth message.
func (vu *VuFs) rauth(r *ConnFcall) string {
	return "not supported"
}

// Response to Stat message.
func (vu *VuFs) rstat(r *ConnFcall) string {
	var err error

	fid, found := r.conn.fids[r.fc.Fid]
	if !found {
		return "fid not found"
	}
	rc.Stat, err = fid.file.Bytes()
	if err != nil {
		return "stat: " + err.Error()
	}
	return ""
}

// Response to Create message.
func (vu *VuFs) rcreate(r *ConnFcall) string {

	// Fid that comes in should point to a directory.
	fid, found := r.conn.fids[r.fc.Fid]
	if !found {
		return "fid not found"
	}
	parent := fid.file
	if parent.Qid.Type&QTDIR == 0 {
		return parent.Name + " not a directory"
	}

	if r.fc.Name == "." || r.fc.Name == ".." {
		return r.fc.Name + " invalid name"
	}

	// User must have permission to write to parent directory.
	if !CheckPerm(fid.file, fid.uid, DMWRITE) {
		return "permission denied"
	}

	// BUG(mbucc) Restrict characters used in a new filename.

	// File should not already exist.
	_, found = parent.children[r.fc.Name]
	if found {
		return "already exists"
	}

	if r.fc.Perm&QTDIR == 1 && r.fc.Mode&3 != OREAD {
		return "can only create a directory in read mode"
	}

	// fcall.go:55,79
	// mode = I/O type, e.g. OREAD.  See const.go:50,61.

	ospath := filepath.Join(vu.Root, parent.Name, r.fc.Name)
	fsyspath := filepath.Join(parent.Name, r.fc.Name)

	goflags := openflags(r.fc.Mode, r.fc.Perm) | os.O_CREATE
	//gomode := os.FileMode(r.fc.Perm & 0777)

	var gomode os.FileMode
	if r.fc.Perm&QTDIR == 1 {
		t0 := parent.Mode & 0777
		t1 := t0 | ^Perm(0777)
		t2 := r.fc.Perm & t1
		gomode = os.FileMode(t2) | os.ModeDir
	} else {
		gomode = os.FileMode(r.fc.Perm & (^Perm(0666) | (parent.Mode & 0666)))
	}


	fp, err := os.OpenFile(ospath, goflags, gomode)
	if err != nil {
		return fsyspath + ": " + err.Error()
	}

	// Owner of new file is user that attached.  Group is from parent directory.
	uid := fid.uid
	gid := parent.Gid
	err = writeOwnership(ospath, uid, gid)
	if err != nil {
		return fsyspath + ": " + err.Error()
	}

	info, err := fp.Stat()
	if err != nil {
		emsg := fsyspath + ": " + err.Error()
		err1 := os.Remove(ospath)
		if err1 != nil {
			emsg += " (and file was left on disk)"
		}
		return emsg
	}
	stat, err := info2stat(info)
	if err != nil {
		emsg := fsyspath + ": " + err.Error()
		err1 := os.Remove(ospath)
		if err1 != nil {
			emsg += " (and file was left on disk)"
		}
		return emsg
	}

	// Times in 9p messages will wrap in 2106.
	now := time.Now()

	// dir.go:60,72
	f := new(File)
	if r.fc.Perm&QTDIR == 1 {
		f.Qid.Type |= QTDIR
	} else {
		f.Qid.Type = QTFILE
	}
	f.Qid.Path = stat.Ino
	f.Qid.Type = uint8(r.fc.Perm >> 24)
	f.Qid.Vers = uint32(now.UnixNano() / 1000000)
	f.Mode = r.fc.Perm
	f.Atime = uint32(now.Unix())
	f.Mtime = uint32(now.Unix())
	f.Length = 0
	f.Name = r.fc.Name
	f.Uid = uid
	f.Gid = gid
	f.Muid = uid
	f.parent = parent
	f.parent.children[f.Name] = f

	r.conn.fids[r.fc.Fid] = &Fid{f, uid, true}
	rc.Type = Rcreate
	rc.Qid = f.Qid

	return ""
}

func CheckPerm(f *File, uid string, perm Perm) bool {

	if uid == "" {
		return false
	}

	perm &= 7

	// other permissions
	fperm := f.Mode & 7
	if (fperm & perm) == perm {

		return true
	}

	// uid permissions
	if f.Uid == uid {
		fperm |= (f.Mode >> 6) & 7
	}

	if (fperm & perm) == perm {

		return true
	}


	// BUG(mbucc) : CheckPerm doesn't consider group.

/*
	// group permissions
	groups := uid.Groups()
	if groups != nil && len(groups) > 0 {
		for i := 0; i < len(groups); i++ {
			if f.Gid == groups[i].Name() {
				fperm |= (f.Mode >> 3) & 7
				break
			}
		}
	}

	if (fperm & perm) == perm {
		return true
	}
*/

	return false
}


// Response to Walk message.
func (vu *VuFs) rwalk(r *ConnFcall) string {

	tx := r.fc

	fid, found := r.conn.fids[tx.Fid]
	if !found {
		return fmt.Sprintf("fid %d not found", tx.Fid)
	}
fmt.Println("walk Wname =", tx.Wname)
fmt.Println("walk: fid.file =", fid.file)
	if len(tx.Wname) > 0 && fid.file.Type & QTDIR != QTDIR {
		return "not a directory"
	}

	if fid.open {
		return "already open"
	}

	if len(tx.Wname) == 0 {
		r.conn.fids[tx.Newfid] = fid
		rc.Wqid = append(rc.Wqid, fid.file.Qid)
		return ""
	}

	_, found = r.conn.fids[tx.Newfid]
	if found {
		return "already in use"
	}
	
	f := fid.file
	for i, wn := range tx.Wname {

		if wn == ".." {
			f = f.parent
		} else {
			if f, found = f.children[wn]; !found {
				if i == 0 {
					return fmt.Sprintf("'%s' not found", wn)
				} else {
					// Return files we have walked, but don't set newfid.
					return ""
				}
			}
	
			if f.Type & QTDIR == 1 && !CheckPerm(f, fid.uid, DMEXEC) {
				if i == 0 {
					return "permission denied"
				} else {
					// Return files we have walked, but don't set newfid.
					return ""
				}
			}
		}

		rc.Wqid = append(rc.Wqid, f.Qid)
	}

	newfid := new(Fid)
	newfid.uid = fid.uid
	newfid.file = f

	r.conn.fids[tx.Newfid] = newfid

	return ""
}
