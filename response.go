package vufs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

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

	fid := new(Fid)
	fid.file = vu.tree.root
	fid.uid = r.fc.Uname
	r.conn.fids[r.fc.Fid] = fid
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

	var err error
	var fp *os.File

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

	// BUG(mbucc) Check characters used in a new filename.

	// File should not already exist.
	_, found = parent.children[r.fc.Name]
	if found {
		return "already exists"
	}

	if r.fc.Perm&QTDIR != 0 && r.fc.Mode&3 != OREAD {
		return "can only create a directory in read mode"
	}

	// fcall.go:55,79
	// See const.go:50,61

	ospath := filepath.Join(vu.Root, parent.Name, r.fc.Name)


	if r.fc.Perm&QTDIR == 0 {
		fp, err = os.OpenFile(ospath, os.O_RDWR|os.O_CREATE,  os.FileMode(r.fc.Perm&0777))
	} else {
		err = os.Mkdir(ospath, os.FileMode(r.fc.Perm&0777))
		if err == nil {
			fp, err = os.OpenFile(ospath, os.O_RDONLY, 0)
		}

	}
	if err != nil {
		return err.Error()
	}

	// Owner of new file is user that attached.  Group is from parent directory.
	uid := fid.uid
	gid := parent.Gid
	err = writeOwnership(ospath, uid, gid)

	if err != nil {
		return err.Error()
	}

	info, err := fp.Stat()
	if err != nil {
		os.Remove(ospath)
		os.Remove(ospath + ".vufs")
		return err.Error()
	}
	stat, err := info2stat(info)
	if err != nil {
		os.Remove(ospath)
		os.Remove(ospath + ".vufs")
		return err.Error()
	}

	// Times in 9p messages will wrap in 2106.
	now := time.Now()

	// dir.go:60,72
	f := new(File)
	if r.fc.Perm&QTDIR == 0 {
		f.Qid.Type = QTFILE
	} else {
		f.Qid.Type |= QTDIR
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
	f.parent.children = make(map[string]*File)
	f.parent.children[f.Name] = f

	f.refcnt = 1
	f.handle = fp

	fid = new(Fid)
	fid.file = f
	fid.uid = uid
	fid.open = true
	fid.mode = r.fc.Mode
	r.conn.fids[r.fc.Fid] = fid

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

	if len(tx.Wname) > 0 && fid.file.Type & QTDIR == 0 {
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
	
			if f.Type & QTDIR != 0 && !CheckPerm(f, fid.uid, DMEXEC) {
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

// Response to a Clunk message.
func (vu *VuFs) rclunk(r *ConnFcall) string {
	var err error

	fid, found := r.conn.fids[r.fc.Fid]
	if !found {
		return "fid not found"
	}

	fid.file.refcnt -= 1
	if fid.file.refcnt == 0 {
		fid.file.handle.Close()
	}


	rc.Stat, err = fid.file.Bytes()
	if err != nil {
		return "stat: " + err.Error()
	}
	return ""
}
