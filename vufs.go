package vufs

import (
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// A Fid is a pointer to a file (a handle) and is unique per connection.
// The uid is set on attach.
type Fid struct {
	uid  string
	file *File
}

type Conn struct {
	rwc   io.ReadWriteCloser
	srv   *VuFs
	dying bool
	fids  map[uint32]*Fid
	msize uint32
}

// A ConnFcall combines a file system call and it's connection.
// The file call handlers need both, as fid's are by connection and
// files are by file system.
type ConnFcall struct {
	conn *Conn
	fc   *Fcall
}

// A File represents a file in the file system, and is unique across the file server.
// Multiple connections may have a handle to the same File.
type File struct {
	Dir
	// TODO(mbucc) Load list when File is a directory.
	list []*File
}

// A Tree is an in-memory representation of the entire File structure.
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

	r.conn.fids[r.fc.Fid] = &Fid{r.fc.Uname, vu.tree.root}
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

	fid, found := r.conn.fids[r.fc.Fid]
	if !found {
		return "fid not found"
	}
	parent := fid.file
	uid := fid.uid
	gid := fid.file.Gid
	if parent.Qid.Type&QTDIR == 0 {
		return parent.Name + " is not a directory"
	}

	if r.fc.Name == "." || r.fc.Name == ".." {
		return r.fc.Name + " invalid name"
	}

	// BUG(mbucc) No check on what characters are used in new filename.

	// See if file already exists.
	for _, fp := range parent.list {
		if fp.Name == r.fc.Name {
			return "already exists"
		}
	}

	if r.fc.Perm&QTDIR == 1 && r.fc.Mode&3 != OREAD {
		return "can only create a directory in read mode"
	}

	// fcall.go:55,79

	// mode = I/O type, e.g. OREAD.  See const.go:50,61.
	ospath := filepath.Join(vu.Root, parent.Name, r.fc.Name)
	fsyspath := filepath.Join(parent.Name, r.fc.Name)
	goflags := openflags(r.fc.Mode, r.fc.Perm) | os.O_CREATE
	gomode := os.FileMode(r.fc.Perm & 0777)
	// Times in 9p messages will wrap in 2106.
	now := uint32(time.Now().Unix())
	fp, err := os.OpenFile(ospath, goflags, gomode)
	if err != nil {
		return fsyspath + ": " + err.Error()
	}
	err = writeOwnership(ospath, uid, gid)
	if err != nil {
		return fsyspath + ": " + err.Error()
	}

	info, err := fp.Stat()
	if err != nil {
		emsg :=  fsyspath + ": " + err.Error()
		err1 := os.Remove(ospath)
		if err1 != nil {
			emsg += " (and file was left on disk)"
		}
		return emsg
	}
	stat, err := info2stat(info)
	if err != nil {
		emsg :=  fsyspath + ": " + err.Error()
		err1 := os.Remove(ospath)
		if err1 != nil {
			emsg += " (and file was left on disk)"
		}
		return emsg
	}
	// dir.go:60,72
	f := new(File)
	f.Qid.Type = QTFILE
	f.Qid.Path = stat.Ino
	f.Qid.Type = uint8(r.fc.Perm >> 24)
	f.Mode = r.fc.Perm
	f.Atime = now
	f.Mtime = now
	f.Length = 0
	f.Name = r.fc.Name
	f.Uid = uid
	f.Gid = gid
	f.Muid = uid

	r.conn.fids[r.fc.Fid] = &Fid{uid, f}
	parent.list = append(parent.list, f)
	rc.Type = Rcreate
	rc.Qid = f.Qid

	return ""
}

// Read file system calls off channel one-by-one.
func (vu *VuFs) fcallhandler() {
	var emsg string
	for !vu.dying {
		x, more := <-vu.fcallchan
		if more {
			emsg = ""
			rc.Reset()
			vu.chat("<- " + x.fc.String())

			// https://github.com/0intro/plan9/blob/7524062cfa4689019a4ed6fc22500ec209522ef0/sys/src/cmd/ip/ftpfs/ftpfs.c#L277-L288

			f, ok := fcallhandlers[x.fc.Type]
			if !ok {
				emsg = "bad fcall type"
			} else {
				emsg = f(x)
			}
			if emsg != "" {
				rc.Type = Rerror
				rc.Ename = emsg
			} else {
				rc.Type = x.fc.Type + 1
				rc.Fid = x.fc.Fid
			}
			rc.Tag = x.fc.Tag
			vu.chat("-> " + rc.String())
			WriteFcall(x.conn.rwc, rc)
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
			c.srv.fcallchan <- &ConnFcall{c, fc}
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
	for !vu.dying {
		vu.chat("connhandler")
		conn, more := <-vu.connchan
		if more {
			c := &Conn{
				rwc:   conn,
				msize: MAX_MSIZE,
				srv:   vu,
				fids:  make(map[uint32]*Fid)}
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
		rc.Ename = "file system stopped"
		rc.Tag = x.fc.Tag
		rc.Type = Rerror
		vu.chat("-> " + rc.String())
		WriteFcall(x.conn.rwc, rc)
	}

	for _, c := range vu.connections {
		c.dying = true
		c.rwc.Close()
	}
	vu.listener.Close()
	<-vu.connchanDone
	<-vu.fcallchanDone
}

func info2stat(info os.FileInfo) (*syscall.Stat_t, error) {
	sysif := info.Sys()
	if sysif == nil {
		return nil, fmt.Errorf("no info.Sys() on this system")
	}
	switch sysif.(type) {
	case *syscall.Stat_t:
		return sysif.(*syscall.Stat_t), nil
	default:
		return nil, fmt.Errorf("invalid info.Sys() on this system")
	}
}

func (f *File) init(rootdir string) error {

	info, err := os.Stat(rootdir)
	if err != nil {
		return err
	}
	stat, err := info2stat(info)
	if err != nil {
		return err
	}

	f.Null()

	f.Qid.Path = stat.Ino
	f.Qid.Vers = uint32(info.ModTime().UnixNano() / 1000000)
	f.Mode = Perm(info.Mode() & 0777)

	f.Atime = uint32(atime(stat).Unix())
	f.Mtime = uint32(info.ModTime().Unix())
	f.Length = uint64(info.Size())
	f.Name = "/" // rootdir[strings.LastIndex(rootdir, "/")+1:]

	if info.IsDir() {
		f.Mode |= DMDIR
		f.Qid.Vers |= QTDIR
		f.Length = 0
	}

	f.Uid = DEFAULT_USER
	f.Gid = DEFAULT_USER
	f.Muid = DEFAULT_USER

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

var fcallhandlers map[uint8]func(*ConnFcall) string

func New(root string) *VuFs {

	vu := new(VuFs)
	vu.Root = root
	vu.connchan = make(chan net.Conn)
	vu.fcallchan = make(chan *ConnFcall)
	vu.connchanDone = make(chan bool)
	vu.fcallchanDone = make(chan bool)

	fcallhandlers = map[uint8](func(*ConnFcall) string){
		Tversion: vu.rversion,
		Tattach:  vu.rattach,
		Tauth:    vu.rauth,
		Tstat:    vu.rstat,
		Tcreate:  vu.rcreate,
	}

	return vu
}
