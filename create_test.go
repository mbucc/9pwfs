/*
   Copyright (c) 2015, Mark Bucciarelli <mkbucc@gmail.com>
*/

package vufs_test

import (
	"github.com/mbucc/vufs"
	"io/ioutil"
	"net"
	"os"
	"testing"
)

type testFile struct {
	name string
	walknames []string
	parentfid uint32
	newfid uint32
	perm vufs.Perm
	mode uint8
	bad bool
}

func (tf *testFile) create(t *testing.T, c net.Conn) *vufs.Fcall {

	// Walk to root directory first so we don't lose the root fid.
	tx := new(vufs.Fcall)
	tx.Type = vufs.Twalk
	tx.Fid = tf.parentfid
	tx.Newfid = tf.newfid
	tx.Tag = 1
	tx.Wname = tf.walknames
	writeTestFcall(t, c, tx)

	tx.Reset()
	tx.Type = vufs.Tcreate
	tx.Fid = tf.newfid
	tx.Tag = 1
	tx.Name = tf.name
	tx.Mode = tf.mode
	tx.Perm = tf.perm

	if tf.bad {
		return writeBadTestFcall(t, c, tx)
	} else {
		return writeTestFcall(t, c, tx)
	}
}

func (tf *testFile) reset() {
	tf.name = ""
	tf.walknames = make([]string, 0)
	tf.parentfid = uint32(0)
	tf.newfid = uint32(0)
	tf.perm = vufs.Perm(0)
	tf.mode = uint8(0)
	tf.bad = false	
}

type testConfig struct {
	rootdir string
	uid string
	rootfid uint32
}

func connectAndAttach(t *testing.T, ts *testConfig) (*vufs.VuFs, net.Conn) {

	fs := vufs.New(ts.rootdir)
	err := fs.Start("tcp", vufs.DEFAULTPORT)
	if err != nil {
		t.Fatalf("start failed: %v", err)
	}
	c, err := net.Dial("tcp", vufs.DEFAULTPORT)
	if err != nil {
		t.Fatalf("connection failed: %v", err)
	}

	tx := new(vufs.Fcall)
	tx.Type = vufs.Tversion
	tx.Tag = vufs.NOTAG
	tx.Msize = 131072
	tx.Version = vufs.VERSION9P
	rx := writeTestFcall(t, c, tx)
	if rx.Version != vufs.VERSION9P {
		t.Fatalf("bad version response, expected '%s' got '%s'", vufs.VERSION9P, rx.Version)
	}

	// Attach to root directory.
	tx.Reset()
	tx.Type = vufs.Tattach
	tx.Fid = ts.rootfid
	tx.Tag = 1
	tx.Afid = vufs.NOFID
	tx.Uname = ts.uid
	tx.Aname = "/"
	rx = writeTestFcall(t, c, tx)

	//fs.Chatty(true)


	return fs, c


}

/*
func setupCreateTest(t *testing.T, fid uint32, rootdir, uid string) (*vufs.VuFs, net.Conn) {

	// Start server and create connection.
	fs := vufs.New(rootdir)
	err := fs.Start("tcp", vufs.DEFAULTPORT)
	if err != nil {
		t.Fatalf("start failed: %v", err)
	}
	c, err := net.Dial("tcp", vufs.DEFAULTPORT)
	if err != nil {
		t.Errorf("connection failed: %v", err)
		return nil, nil
	}

	// Send version message.
	tx := &vufs.Fcall{
		Type:    vufs.Tversion,
		Tag:     vufs.NOTAG,
		Msize:   131072,
		Version: vufs.VERSION9P}
	rx := writeTestFcall(t, c, tx)
	if rx.Version != vufs.VERSION9P {
		t.Errorf("bad version response, expected '%s' got '%s'", vufs.VERSION9P, rx.Version)
		return nil, nil
	}

	// Attach to root directory.
	tx = &vufs.Fcall{
		Type:  vufs.Tattach,
		Fid:   fid,
		Tag:   1,
		Afid:  vufs.NOFID,
		Uname: uid,
		Aname: "/"}
	rx = writeTestFcall(t, c, tx)
	//fs.Chatty(true)


	return fs, c

}
*/

// Can adm create a subdirectory off root?   (Yes.)
func TestCreate(t *testing.T) {

	rootdir, err := ioutil.TempDir("", "testcreate")
	if err != nil {
		t.Fatalf("TempDir failed: %v", err)
	}
	defer os.RemoveAll(rootdir)


	newfid := uint32(2)

	config := new(testConfig)
	config.rootdir = rootdir
	config.rootfid = 1
	config.uid = "mark"
	fs, c := connectAndAttach(t, config)
	if fs == nil || c == nil {
		return
	}
	defer fs.Stop()
	defer c.Close()

	tf := new(testFile)
	tf.name = "testcreate.txt"
	tf.walknames = make([]string, 0)
	tf.parentfid = config.rootfid
	tf.newfid = newfid
	tf.perm = vufs.Perm(0644)
	tf.mode = vufs.OREAD
	rx := tf.create(t, c)
	
	// Qid should be loaded
	if rx.Qid.Path == 0 {
		t.Error("Qid.Path was zero")
	}

	// Stat newly created file.
	tx := &vufs.Fcall{Type: vufs.Tstat, Fid: newfid, Tag: 1}
	rx = writeTestFcall(t, c, tx)
	dir, err := vufs.UnmarshalDir(rx.Stat)
	if err != nil {
		t.Fatalf("UnmarshalDir failed: %v", rx.Ename)
	}

	// User of file should be same as user passed in
	if dir.Uid != config.uid {
		t.Errorf("wrong user, expected '%s' got '%s'", config.uid, dir.Uid)
	}

	if dir.Name != "testcreate.txt" {
		t.Errorf("wrong Name, expected '%s', got '%s'", "testcreate.txt", dir.Name)
	}

	if dir.Length != 0 {
		t.Errorf("newly created empty file should have length 0")
	}

	// Group of file is from directory group.
	if dir.Gid != vufs.DEFAULT_USER {
		t.Errorf("wrong group, expected '%s' got '%s'", vufs.DEFAULT_USER, dir.Uid)
	}
}


func TestFailIfFileAlreadyExists(t *testing.T) {

	rootdir, err := ioutil.TempDir("", "testcreate")
	if err != nil {
		t.Fatalf("TempDir failed: %v", err)
	}
	defer os.RemoveAll(rootdir)

	config := new(testConfig)
	config.rootdir = rootdir
	config.rootfid = 1
	config.uid = "mark"
	fs, c := connectAndAttach(t, config)
	if fs == nil || c == nil {
		return
	}
	defer fs.Stop()
	defer c.Close()

	tf := new(testFile)
	tf.name = "testcreate.txt"
	tf.walknames = make([]string, 0)
	tf.parentfid = config.rootfid
	tf.newfid = 2
	tf.perm = vufs.Perm(0644)
	tf.mode = vufs.OREAD

	tf.create(t, c)

	tf.bad = true
	rx := tf.create(t, c)

	if rx.Ename != "already exists" {
		t.Fatalf("expected '%s', got '%s'", "already exists", rx.Ename)
	}
}


func TestClampPermissionsToParentDirectory(t *testing.T) {

	rootdir, err := ioutil.TempDir("", "testcreate")
	if err != nil {
		t.Fatalf("TempDir failed: %v", err)
	}
	defer os.RemoveAll(rootdir)

	config := new(testConfig)
	config.rootdir = rootdir
	config.rootfid = 1
	config.uid = "mark"
	fs, c := connectAndAttach(t, config)
	if fs == nil || c == nil {
		return
	}
	defer fs.Stop()
	defer c.Close()

	// Create '/testdir', with 0700 permissions.
	dirfid := uint32(2)
	tf := new(testFile)
	tf.name = "testdir"
	tf.walknames = make([]string, 0)
	tf.parentfid = config.rootfid
	tf.newfid = dirfid
	tf.perm = vufs.Perm(0700) | vufs.DMDIR
	tf.mode = vufs.OREAD
	tf.create(t, c)

	// Clunk the new directory, as a walk will fail on an open fid.
	tx := new(vufs.Fcall)
	tx.Type = vufs.Tclunk
	tx.Fid = dirfid
	tx.Tag = 1
	writeTestFcall(t, c, tx)

	// Create '/testdir/test.txt'
	tf.reset()
	tf.name = "test.txt"
	tf.walknames = []string{"testdir"}
	tf.parentfid = config.rootfid
	tf.newfid = dirfid
	tf.perm = vufs.Perm(0666)
	tf.mode = vufs.OREAD
	tf.create(t, c)


	// Test that file permissions are 0600
	tx = &vufs.Fcall{Type: vufs.Tstat, Fid: dirfid, Tag: 1}
	rx := writeTestFcall(t, c, tx)
	dir, err := vufs.UnmarshalDir(rx.Stat)
	if err != nil {
		t.Fatalf("UnmarshalDir failed: %v", rx.Ename)
	}
	if dir.Mode != vufs.Perm(0600) {
		t.Errorf("new file permissions not clamped: got %s not %s", dir.Mode, vufs.Perm(0600))
	}
}

// File system uses .vufs extension to store permissions. 
// Don't allow files to be created with this extension.
func TestFailIfFileUsesMagicExtension(t *testing.T) {

	rootdir, err := ioutil.TempDir("", "testcreate")
	if err != nil {
		t.Fatalf("TempDir failed: %v", err)
	}
	defer os.RemoveAll(rootdir)

	config := new(testConfig)
	config.rootdir = rootdir
	config.rootfid = 1
	config.uid = "mark"
	fs, c := connectAndAttach(t, config)
	defer fs.Stop()
	defer c.Close()

	tf := new(testFile)
	tf.name = "testcreate.vufs"
	tf.walknames = make([]string, 0)
	tf.parentfid = 1
	tf.newfid = 2
	tf.perm = vufs.Perm(0644)
	tf.mode = vufs.OREAD
	tf.bad = true

	rx := tf.create(t, c)

	if rx.Ename != "invalid file name" {
		t.Fatalf("expected '%s', got '%s'", "invalid file name", rx.Ename)
	}
}

