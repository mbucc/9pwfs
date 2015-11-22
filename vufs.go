package vufs

import (
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"syscall"
)

// A Fid is a pointer to a file (a handle) and is unique per connection.
// The uid is set on attach.
type Fid struct {
	file *File
	uid  string
	open bool
	// See const.go:50,61
	mode    uint8
}

// A File represents a file in the file system, and is unique across the file server.
// Multiple connections may have a handle to the same File.
// Only one file handle is opened for all connections and their clients.
// When a clunk results in a refcnt of zero, the file handle is closed.
type File struct {
	// dir.go:60,72
	Dir
	parent *File
	children map[string]*File
	// This is always read/write.  The Fid stores if the file was opened read or read/write.
	handle *os.File 
	refcnt int
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

func (vu *VuFs) log(msg string) {
	fmt.Println("vufs: " + msg)
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
				c.srv.log("recv() error: " + err.Error())
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

func (vu *VuFs) buildfile(ospath string, info os.FileInfo) (*File, error) {

	var found bool

	stat, err := info2stat(info)
	if err != nil {
		return nil, err
	}

	f := new(File)
	f.Null()

	f.Qid.Path = stat.Ino
	f.Qid.Vers = uint32(info.ModTime().UnixNano() / 1000000)
	// BUG(mbucc) We drop all higher file mode bits when loading tree.
	f.Mode = Perm(info.Mode() & 0777)

	f.Atime = uint32(atime(stat).Unix())
	f.Mtime = uint32(info.ModTime().Unix())
	f.Length = uint64(info.Size())
	f.Name = info.Name()
	f.children = make(map[string]*File)

	if info.IsDir() {
		f.Mode |= DMDIR
		f.Qid.Vers |= QTDIR
		f.Length = 0
	}

	if ospath != vu.Root {
		parentpath := filepath.Join(ospath, "..")
		f.parent, found = loadmap[parentpath]
		if !found {
			return nil, fmt.Errorf("parent '%s' not in loadmap for '%s'", parentpath, ospath)
		}
		f.parent.children[f.Name] = f
	} else {
		f.Name = "/"
		f.parent = f

		// Hard code the mode of root directory to 0777.
		// This way, you have to sudo to the user that is running the file
		// system daemon to "manually" manipulate the files in the file sys.
		// Not real security, but a convenience to avoid stupid mistakes.
		f.Mode = 0777
	}

	// BUG(mbucc) Look up [u|g|mu]id from <path>.vufs
	f.Uid = DEFAULT_USER
	f.Gid = DEFAULT_USER
	f.Muid = DEFAULT_USER

	return f, nil
}


func (vu *VuFs) buildnode(path string, info os.FileInfo, err error) error {

	if err != nil {
		return err
	}

	f, err := vu.buildfile(path, info)

	if err != nil {
		return err
	}
	loadmap[path] = f

	return nil

}

var loadmap map[string]*File

func (vu *VuFs) buildtree() error {

	//t0 := time.Now()

	loadmap = make(map[string]*File, 100000)
	err := filepath.Walk(vu.Root, vu.buildnode)
	if err != nil {
		return err
	}
	
	f, found := loadmap[vu.Root]
	if !found {
		return fmt.Errorf("didn't load file for root dir '%s'", vu.Root)
	}

	vu.tree = &Tree{f}

    	//t1 := time.Now()

/*
// TODO: Too chatty for tests; put in read-only /stats file (or similar)
	if len(loadmap) == 1 {
		vu.log(fmt.Sprintf("loaded 1 file in %v", t1.Sub(t0)))
	} else {
		vu.log(fmt.Sprintf("Loaded %d files in %v", len(loadmap), t1.Sub(t0)))
	}
*/

	return nil
}

// Stop listening, drain channels, wait any in-progress work to finish, and shut down.
func (vu *VuFs) Stop() {
	vu.Lock()
	defer vu.Unlock()

	vu.dying = true
	close(vu.connchan)
	for _, c := range vu.connections {
		c.dying = true
		c.rwc.Close()
	}

	close(vu.fcallchan)
	for x := range vu.fcallchan {
		rc.Ename = "file system stopped"
		rc.Tag = x.fc.Tag
		rc.Type = Rerror
		vu.chat("-> " + rc.String())
		WriteFcall(x.conn.rwc, rc)
	}

	vu.listener.Close()
	<-vu.connchanDone
	<-vu.fcallchanDone
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

var fcallhandlers map[uint8]func(*ConnFcall) string

func New(root string) *VuFs {

	vu := new(VuFs)
	vu.Root = root
	vu.log("creating filesystem rooted at " + root)
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
		Twalk:  vu.rwalk,
		Tclunk:  vu.rclunk,
	}

	return vu
}
