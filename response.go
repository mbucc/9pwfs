package vufs

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
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

// TODO(mbucc) Decide and enforce what characters are valid in filenames.
func validFilename(name string) bool {
	return name != "." && name != ".." && !strings.HasSuffix(name, ".vufs")
}

// Since we serialize all file operations, we can reuse the same response memory.
var rc *Fcall = new(Fcall)

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

	rc.Data = make([]byte, 0, msz)

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

func (vu *VuFs) rattach(r *ConnFcall) string {

	// To simplify things, we only allow an attach to root of file server.
	if r.fc.Aname != "/" {
		return "can only attach to root directory"
	}

	// We don't support authentication.
	if r.fc.Afid != NOFID {
		return "authentication not supported"
	}

	_, emsg := r.conn.findfid(r.fc.Fid)
	if emsg == "" {
		return "fid already in use on this connection"
	}
	if emsg == "phase shift" {
		return emsg
	}

	fid := new(Fid)
	fid.file = vu.tree.root
	fid.uid = r.fc.Uname
	r.conn.fids[r.fc.Fid] = fid
	rc.Qid = vu.tree.root.Qid
	return ""
}

func (vu *VuFs) rauth(r *ConnFcall) string {
	return "not supported"
}

func (vu *VuFs) rstat(r *ConnFcall) string {
	var err error

	fid, emsg := r.conn.findfid(r.fc.Fid)
	if emsg != "" {
		return emsg
	}
	rc.Stat, err = fid.file.Bytes()
	if err != nil {
		return "stat: " + err.Error()
	}
	return ""
}

// Vufs only supports READ and WRITE modes.  (CEXEC is equivalent to READ.)
func checkMode(fc *Fcall) string {
	// fcall.go:55,79
	// See const.go:50,61
	if fc.Perm&DMDIR != 0 && fc.Mode != OREAD {
		return "invalid mode for a directory"
	}
	if fc.Mode&OTRUNC != 0 {
		return "OTRUNC not supported"
	}
	if fc.Mode&ORCLOSE != 0 {
		return "ORCLOSE not supported"
	}
	if fc.Mode&ODIRECT != 0 {
		return "ODIRECT not supported"
	}
	return ""
}

func (vu *VuFs) rcreate(r *ConnFcall) string {

	var err error
	var fp *os.File

	fid, emsg := r.conn.findfid(r.fc.Fid)
	if emsg != "" {
		return emsg
	}
	parent := fid.file

	if !validFilename(r.fc.Name) {
		return "invalid file name"
	}

	// User must have permission to write to parent directory.
	if !CheckPerm(fid.file, fid.uid, DMWRITE) {
		return "permission denied"
	}

	// File should not already exist.
	if _, found := parent.children[r.fc.Name]; found {
		return "already exists"
	}

	if emsg := checkMode(r.fc); emsg != "" {
		return emsg
	}

	var mode Perm
	ospath := filepath.Join(vu.Root, parent.Name, r.fc.Name)
	if r.fc.Perm&DMDIR != 0 {
		mode = r.fc.Perm & (^Perm(0777) | (parent.Mode & Perm(0777)))
		err = os.Mkdir(ospath, os.FileMode(mode&0777))
		if err != nil {
			return err.Error()
		}
		fp, err = os.OpenFile(ospath, os.O_RDONLY, 0)
		if err != nil {
			os.Remove(ospath)
			return err.Error()
		}
	} else {
		mode = r.fc.Perm & (^Perm(0666) | (parent.Mode & Perm(0666)))
		// Open file as read/write so we only need one file handle
		// no matter how many clients.  Store the mode on
		// the Fid (per connection) and the handle on the File
		// (per file server).
		fp, err = os.OpenFile(ospath, os.O_RDWR|os.O_CREATE, os.FileMode(mode&0777))
		if err != nil {
			return err.Error()
		}
	}

	// Owner of new file is user that attached.  Group is from parent directory.
	uid := fid.uid
	gid := parent.Gid
	err = writeOwnership(ospath, uid, gid)
	if err != nil {
		fp.Close()
		return err.Error()
	}

	// We use Inode as identifier in Qid, so we need to stat file.
	info, err := fp.Stat()
	if err != nil {
		fp.Close()
		os.Remove(ospath)
		os.Remove(ospath + ".vufs")
		return err.Error()
	}
	stat, err := info2stat(info)
	if err != nil {
		fp.Close()
		os.Remove(ospath)
		os.Remove(ospath + ".vufs")
		return err.Error()
	}

	// Times in 9p messages will wrap in 2106.  I'll be long gone.
	now := time.Now()

	// dir.go:60,72
	f := new(File)
	f.ospath = ospath
	if r.fc.Perm&DMDIR != 0 {
		f.Qid.Type = QTDIR
		f.children = make(map[string]*File)
	} else {
		f.Qid.Type = QTFILE
	}
	f.Qid.Path = stat.Ino
	f.Qid.Type = uint8(r.fc.Perm >> 24)
	f.Qid.Vers = uint32(now.UnixNano() / 1000000)
	f.Mode = mode
	f.Atime = uint32(now.Unix())
	f.Mtime = uint32(now.Unix())
	f.Length = 0
	f.Name = r.fc.Name
	f.Uid = uid
	f.Gid = gid
	f.Muid = uid

	f.parent = parent
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

func (vu *VuFs) rwalk(r *ConnFcall) string {

	tx := r.fc

	fid, emsg := r.conn.findfid(r.fc.Fid)
	if emsg != "" {
		return emsg
	}

	if len(tx.Wname) > 0 && !fid.file.isDir() {
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

	if _, found := r.conn.fids[tx.Newfid]; found {
		return "already in use"
	}

	f := fid.file
	for i, wn := range tx.Wname {
		var found bool

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

			if f.isDir() && !CheckPerm(f, fid.uid, DMEXEC) {
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

func (vu *VuFs) rclunk(r *ConnFcall) string {

	fid, emsg := r.conn.findfid(r.fc.Fid)
	if emsg != "" {
		return emsg
	}

	fid.file.refcnt -= 1
	if fid.file.refcnt == 0 && fid.file.handle != nil {
		fid.file.handle.Close()
		fid.file.handle = nil
	}

	delete(r.conn.fids, r.fc.Fid)

	return ""
}

func (vu *VuFs) rwrite(r *ConnFcall) string {

	fid, emsg := r.conn.findfid(r.fc.Fid)
	if emsg != "" {
		return emsg
	}

	if !fid.open {
		return "not open"
	}

	if fid.mode&OWRITE == 0 {
		return "not opened for writing"
	}

	if fid.file.isDir() {
		return "can't write to a directory"
	}

	n, err := fid.file.handle.WriteAt(r.fc.Data, int64(r.fc.Offset))
	rc.Count = uint32(n)
	if err != nil {
		return err.Error()
	}

	now := uint32(time.Now().Unix())
	fid.file.Atime = now
	fid.file.Mtime = now
	// BUG(mbucc): Muid info is lost on server restart.
	fid.file.Muid = fid.uid
	info, err := fid.file.handle.Stat()
	if err != nil {
		return err.Error()
	}
	fid.file.Length = uint64(info.Size())

	return ""
}

func (vu *VuFs) rread(r *ConnFcall) string {

	fid, emsg := r.conn.findfid(r.fc.Fid)
	if emsg != "" {
		return emsg
	}

	if !fid.open {
		return "not open"
	}

	rc.Data = rc.Data[:0]

	if r.fc.Count > uint32(cap(rc.Data)) {
		return "invalid count"
	}

	if fid.file.isDir() {

		keys := make([]string, 0, len(fid.file.children))
		for k := range fid.file.children {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		offset := r.fc.Offset
		count := uint64(r.fc.Count)
		bytesread := uint64(0)
		for _, k := range keys {
			f := fid.file.children[k]
			b, _ := f.Bytes()
			n := uint64(len(b))
			if bytesread >= offset && bytesread+n < offset+count {
				if len(rc.Data) == 0 && bytesread != offset {
					return "invalid offset"
				}
				rc.Data = append(rc.Data, b...)
			}
			bytesread += n
			if bytesread >= offset+count {
				break
			}
		}
	} else {

		if r.fc.Offset >= fid.file.Length {
			return ""
		}

		rc.Data = rc.Data[:r.fc.Count]
		sz, err := fid.file.handle.ReadAt(rc.Data, int64(r.fc.Offset))
		if err != nil && err != io.EOF {
			return err.Error()
		}
		rc.Data = rc.Data[:sz]
	}
	rc.Count = uint32(len(rc.Data))

	fid.file.Atime = uint32(time.Now().Unix())

	return ""
}

func (vu *VuFs) ropen(r *ConnFcall) string {
	var err error

	fid, emsg := r.conn.findfid(r.fc.Fid)
	if emsg != "" {
		return emsg
	}

	if emsg := checkMode(r.fc); emsg != "" {
		return emsg
	}

	m := r.fc.Mode & 3

	if m&OWRITE == OWRITE {
		if !CheckPerm(fid.file, fid.uid, DMWRITE) {
			return "permission denied"
		}
	}
	if m&ORDWR == ORDWR {
		if !CheckPerm(fid.file, fid.uid, DMWRITE) || !CheckPerm(fid.file, fid.uid, DMREAD) {
			return "permission denied"
		}
	}
	if m&OREAD == OREAD {
		if !CheckPerm(fid.file, fid.uid, DMREAD) {
			return "permission denied"
		}
	}
	if m&OEXEC == OEXEC {
		if !CheckPerm(fid.file, fid.uid, DMEXEC) {
			return "permission denied"
		}
	}

	if fid.file.handle == nil {
		var fp *os.File

		if fid.file.isDir() {
			fp, err = os.OpenFile(fid.file.ospath, os.O_RDONLY, 0)
			if err != nil {
				return err.Error()
			}
		} else {
			fp, err = os.OpenFile(fid.file.ospath, os.O_RDWR, 0644)
			if err != nil {
				return err.Error()
			}
		}
		fid.file.handle = fp
	}
	fid.file.refcnt += 1

	fid.open = true
	fid.mode = r.fc.Mode

	return ""
}

func (vu *VuFs) rremove(r *ConnFcall) string {

	fid, emsg := r.conn.findfid(r.fc.Fid)
	if emsg != "" {
		return emsg
	}

	if !CheckPerm(fid.file.parent, fid.uid, DMWRITE) {
		return "permission denied"
	}

	if fid.file.handle != nil {
		if err := fid.file.handle.Close(); err != nil {
			return err.Error()
		}
	}

	if err := os.Remove(fid.file.ospath); err != nil {
		return err.Error()
	}

	delete(fid.file.parent.children, fid.file.Name)

	*(fid.file) = File{}

	delete(r.conn.fids, r.fc.Fid)

	return ""
}

func (vu *VuFs) rwstat(r *ConnFcall) string {

	fid, emsg := r.conn.findfid(r.fc.Fid)
	if emsg != "" {
		return emsg
	}

	dir, err := UnmarshalDir(r.fc.Stat)
	if err != nil {
		return err.Error()
	}

	if dir.Name != "" {
		if !CheckPerm(fid.file.parent, fid.uid, DMWRITE) {
			return "permission denied"
		}
		if !validFilename(dir.Name) {
			return "invalid file name"
		}
		if _, found := fid.file.parent.children[r.fc.Name]; found {
			return "already exists"
		}

		oldp := fid.file.Name
		newp := filepath.Join(oldp, "..", dir.Name)

		// close file
		if fid.file.handle != nil {
			fid.file.handle.Close()
			if err != nil {
				return err.Error()
			}
			fid.file.handle = nil
		}

		// move file
		err = os.Rename(oldp, newp)
		if err != nil {
			return err.Error()
		}

		// move meta file
		err = os.Rename(oldp+".vufs", newp+".vufs")
		if err != nil {
			os.Rename(newp, oldp)
			return err.Error()
		}

		// Open "new" file.
		fid.file.handle, err = os.OpenFile(fid.file.ospath, os.O_RDWR, 0777)
		if err != nil {
			os.Rename(newp, oldp)
			os.Rename(newp+".vufs", oldp+".vufs")
			return err.Error()
		}

		// update in-memory data
		fid.file.ospath = filepath.Join(fid.file.ospath, "..", dir.Name)
		delete(fid.file.parent.children, oldp)
		fid.file.parent.children[newp] = fid.file
	}

	return ""
}
