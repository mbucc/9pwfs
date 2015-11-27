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

func createFile(t *testing.T, c net.Conn, name string, fid, newfid uint32, tag uint16, isdir bool) *vufs.Fcall {

	// Walk to root directory first so we don't lose the root fid.
	tx := new(vufs.Fcall)
	tx.Type = vufs.Twalk
	tx.Fid = fid
	tx.Newfid = newfid
	tx.Tag = tag
	tx.Wname = []string{}
	writeTestFcall(t, c, tx)

	tx.Type = vufs.Tcreate
	tx.Fid = newfid
	tx.Tag = tag
	tx.Name = name
	tx.Mode = 0
	if isdir {
		tx.Perm = vufs.Perm(0775) | vufs.DMDIR
	} else {
		tx.Perm = vufs.Perm(0644)
	}
	return writeTestFcall(t, c, tx)

}

// Can adm create a subdirectory off root?   (Yes.)
func TestCreate(t *testing.T) {

	rootdir, err := ioutil.TempDir("", "testcreate")
	if err != nil {
		t.Fatalf("TempDir failed: %v", err)
	}
	defer os.RemoveAll(rootdir)

	uid := "mark"
	fid := uint32(1)
	newfid := uint32(2)
	fs, c := setupCreateTest(t, fid, rootdir, uid)
	if fs == nil || c == nil {
		return
	}
	defer fs.Stop()
	defer c.Close()

	name := "testcreate.txt"
	tag := uint16(1)
	rx := createFile(t, c, name, fid, newfid, tag, false)

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
	if dir.Uid != uid {
		t.Errorf("wrong user, expected '%s' got '%s'", uid, dir.Uid)
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

	uid := "mark"
	rootfid := uint32(1)
	fs, c := setupCreateTest(t, rootfid, rootdir, uid)
	if fs == nil || c == nil {
		return
	}
	defer fs.Stop()
	defer c.Close()

	name := "testcreate.txt"

	// Walk to root directory first so we don't lose the root fid.
	tx := new(vufs.Fcall)
	tx.Type = vufs.Twalk
	tx.Fid = rootfid
	tx.Newfid = 2
	tx.Tag = 1
	tx.Wname = []string{}
	writeTestFcall(t, c, tx)

	// Create file.
	tx.Reset()
	tx.Type = vufs.Tcreate
	tx.Fid = 2
	tx.Tag = 1
	tx.Name = name
	tx.Mode = 0
	tx.Perm = vufs.Perm(0644)
	writeTestFcall(t, c, tx)

	// Walk to root directory (again, prepping for create call).
	tx.Reset()
	tx.Type = vufs.Twalk
	tx.Fid = rootfid
	tx.Newfid = 3
	tx.Tag = 1
	tx.Wname = []string{}
	writeTestFcall(t, c, tx)

	// Try to create same file again.
	tx.Reset()
	tx.Type = vufs.Tcreate
	tx.Fid = 3
	tx.Tag = 1
	tx.Name = name
	tx.Mode = 0
	tx.Perm = vufs.Perm(0644)
	rx := writeBadTestFcall(t, c, tx)
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

	uid := "mark"
	rootfid := uint32(1)
	fs, c := setupCreateTest(t, rootfid, rootdir, uid)
	if fs == nil || c == nil {
		return
	}
	defer fs.Stop()
	defer c.Close()

	// Walk to file system root to create fid for subdirectory we are creating.
	// Can't use root fid, as Tcreate moves the fid to reference the newly created file.
	dirfid := uint32(2)
	tag := uint16(1)
	tx := new(vufs.Fcall)
	tx.Type = vufs.Twalk
	tx.Fid = rootfid
	tx.Newfid = dirfid
	tx.Tag = tag
	tx.Wname = []string{}
	writeTestFcall(t, c, tx)

	// Create /testdir.
	tx.Reset()
	tx.Type = vufs.Tcreate
	tx.Fid = dirfid
	tx.Tag = tag
	tx.Name = "testdir"
	tx.Mode = vufs.OREAD
	tx.Perm = vufs.Perm(0700) | vufs.DMDIR
	writeTestFcall(t, c, tx)

	// Clunk the new directory, as a walk will fail on an open fid.
	tx.Reset()
	tx.Type = vufs.Tclunk
	tx.Fid = dirfid
	tx.Tag = tag
	writeTestFcall(t, c, tx)

	// Walk to the new directory to get a fid for the subsequent create call.
	tx.Reset()
	tx.Type = vufs.Twalk
	tx.Fid = rootfid
	tx.Newfid = dirfid
	tx.Tag = tag
	tx.Wname = []string{"testdir"}
	writeTestFcall(t, c, tx)


	// Create file /testdir/test.txt.  We can use dirfid, as it points to parent directory.
	tx.Reset()
	tx.Type = vufs.Tcreate
	tx.Fid = dirfid
	tx.Tag = tag
	tx.Name = "test.txt"
	tx.Mode = vufs.OREAD
	tx.Perm = vufs.Perm(0666)
	writeTestFcall(t, c, tx)


	// Test that file perm is 0600 (perms are clamped by parent dir)
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

	uid := "mark"
	fid := uint32(1)
	newfid := uint32(2)
	fs, c := setupCreateTest(t, fid, rootdir, uid)
	if fs == nil || c == nil {
		return
	}
	defer fs.Stop()
	defer c.Close()

	// Walk to root directory first so we don't lose the root fid.
	tx := new(vufs.Fcall)
	tx.Type = vufs.Twalk
	tx.Fid = fid
	tx.Newfid = newfid
	tx.Tag = 1
	tx.Wname = []string{}
	writeTestFcall(t, c, tx)

	// Create a file with the "magic" .vufs extension.
	tx.Reset()
	tx.Type = vufs.Tcreate
	tx.Fid = newfid
	tx.Tag = 1
	tx.Name = "testcreate.vufs"
	tx.Mode = 0
	tx.Perm = vufs.Perm(0644)
	rx := writeBadTestFcall(t, c, tx)

	if rx.Ename != "invalid file name" {
		t.Fatalf("expected '%s', got '%s'", "invalid file name", rx.Ename)
	}
}

