/*
   Copyright (c) 2015, Mark Bucciarelli <mkbucc@gmail.com>
*/

package vufs_test

import (
	"github.com/mbucc/vufs"
	"io/ioutil"
	"fmt"
	"net"
	"os"
	"testing"
)

func setup_create_test(t *testing.T, fid uint32, rootdir, uid string) (*vufs.VuFs, net.Conn) {

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
	err = vufs.WriteFcall(c, tx)
	if err != nil {
		t.Errorf("connection write failed: %v", err)
		return nil, nil
	}
	rx, err := vufs.ReadFcall(c)
	if err != nil {
		t.Errorf("connection read failed: %v", err)
		return nil, nil
	}
	if rx.Type != vufs.Rversion {
		t.Errorf("bad message type, expected %d got %d", vufs.Rversion, rx.Type)
		return nil, nil
	}
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
	err = vufs.WriteFcall(c, tx)
	if err != nil {
		t.Fatalf("Tattach write failed: %v", err)
	}
	rx, err = vufs.ReadFcall(c)
	if err != nil {
		t.Errorf("Rattach read failed: %v", err)
	}
	if rx.Type == vufs.Rerror {
		t.Fatalf("Tattach returned error: '%s'", rx.Ename)
	}
	if rx.Type != vufs.Rattach {
		t.Errorf("bad message type, expected %d got %d", vufs.Rattach, rx.Type)
	}

	return fs, c

}

func createFile(c net.Conn, name string, fid, newfid uint32, tag uint16, isdir bool) error {

	// Walk to root directory first so we don't lose the root fid.
	tx := new(vufs.Fcall)
	tx.Type = vufs.Twalk
	tx.Fid = fid
	tx.Newfid = newfid
	tx.Tag = tag
	tx.Wname = []string{}
	if err := vufs.WriteFcall(c, tx); err != nil {
		return err
	}
	rx, err := vufs.ReadFcall(c)
	if err != nil {
		return err
	}
	if rx.Type == vufs.Rerror {
		return fmt.Errorf("Twalk returned error: '%s'", rx.Ename)
	}
	if rx.Type != vufs.Rwalk {
		return fmt.Errorf("bad message type, expected %d got %d", vufs.Rattach, rx.Type)
	}

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
	return vufs.WriteFcall(c, tx)

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
	fs, c := setup_create_test(t, fid, rootdir, uid)
	if fs == nil || c == nil {
		return
	}
	defer fs.Stop()
	defer c.Close()

	//fs.Chatty(true)

	name := "testcreate.txt"
	tag := uint16(1)
	err = createFile(c, name, fid, newfid, tag, false)
	if err != nil {
		t.Fatalf("Tcreate failed: %v", err)
	}

	rx, err := vufs.ReadFcall(c)
	if err != nil {
		t.Errorf("Rcreate read failed: %v", err)
	}
	if rx.Type == vufs.Rerror {
		t.Fatalf("create returned error: '%s'", rx.Ename)
	}
	if rx.Type != vufs.Rcreate {
		t.Errorf("bad message type, expected %d got %d", vufs.Rcreate, rx.Type)
	}

	// Tag must be the same
	if rx.Tag != tag {
		t.Errorf("wrong tag, expected %d got %d", tag, rx.Tag)
	}

	// Qid should be loaded
	if rx.Qid.Path == 0 {
		t.Error("Qid.Path was zero")
	}

	// Stat newly created file.
	tx := &vufs.Fcall{Type: vufs.Tstat, Fid: newfid, Tag: 1}
	err = vufs.WriteFcall(c, tx)
	if err != nil {
		t.Fatalf("Tstat write failed: %v", err)
	}
	rx, err = vufs.ReadFcall(c)
	if err != nil {
		t.Errorf("Rstat read failed: %v", err)
	}
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
	fid := uint32(1)
	fs, c := setup_create_test(t, fid, rootdir, uid)
	if fs == nil || c == nil {
		return
	}
	defer fs.Stop()
	defer c.Close()

	//fs.Chatty(true)

	name := "testcreate.txt"
	tag := uint16(1)
	err = createFile(c, name, fid, uint32(2), tag, false)
	if err != nil {
		t.Fatalf("Tcreate failed: %v", err)
	}

	_, err = vufs.ReadFcall(c)
	if err != nil {
		t.Fatalf("Rcreate read failed: %v", err)
	}

	err = createFile(c, name, fid, uint32(3), tag, false)
	if err != nil {
		t.Fatalf("Tcreate write failed: %v", err)
	}

	rx, err := vufs.ReadFcall(c)
	if err != nil {
		t.Errorf("Rcreate read failed: %v", err)
	}
	if rx.Type != vufs.Rerror {
		t.Fatalf("Tcreate should fail if file already exists")
	}
	if rx.Ename != "already exists" {
		t.Fatalf("expected '%s', got '%s'", "already exists", rx.Ename)
	}
}


func TestClampPermissionsToParentDirectory(t *testing.T) {

	rootdir, err := ioutil.TempDir("", "testcreate")
	if err != nil {
		t.Fatalf("TempDir failed: %v", err)
	}
	//defer os.RemoveAll(rootdir)

	uid := "mark"
	rootfid := uint32(1)
	fs, c := setup_create_test(t, rootfid, rootdir, uid)
	if fs == nil || c == nil {
		return
	}
	defer fs.Stop()
	defer c.Close()

	//fs.Chatty(true)

	// Walk to parent directory of new subdir (file system root).
	// This loads a fid for new directory that we will use in create call.
	dirfid := uint32(2)
	tag := uint16(1)
	tx := new(vufs.Fcall)
	tx.Type = vufs.Twalk
	tx.Fid = rootfid
	tx.Newfid = dirfid
	tx.Tag = tag
	tx.Wname = []string{}
	if err := vufs.WriteFcall(c, tx); err != nil {
		t.Fatalf("Twalk failed: %v", err)
	}
	rx, err := vufs.ReadFcall(c)
	if err != nil {
		t.Fatalf("reading Rwalk failed: %v", err)
	}
	if rx.Type == vufs.Rerror {
		t.Errorf("Twalk returned error: '%s'", rx.Ename)
	}

	// Create /testdir.
	tx.Reset()
	tx.Type = vufs.Tcreate
	tx.Fid = dirfid
	tx.Tag = tag
	tx.Name = "testdir"
	tx.Mode = vufs.OREAD
	tx.Perm = vufs.Perm(0700) | vufs.DMDIR
	if err := vufs.WriteFcall(c, tx); err != nil {
		t.Fatalf("Tcreate failed: %v", err)
	}
	rx, err = vufs.ReadFcall(c)
	if err != nil {
		t.Fatalf("reading Rcreate failed: %v", err)
	}
	if rx.Type == vufs.Rerror {
		t.Fatalf("Tcreate returned error: '%s'", rx.Ename)
	}


	// Clunk the new directory, as a walk will fail if the fid passed in is open.
	tx.Reset()
	tx.Type = vufs.Tclunk
	tx.Fid = dirfid
	tx.Tag = tag
	if err := vufs.WriteFcall(c, tx); err != nil {
		t.Fatalf("Tclunk failed: %v", err)
	}
	rx, err = vufs.ReadFcall(c)
	if err != nil {
		t.Fatalf("reading Rclunk failed: %v", err)
	}
	if rx.Type == vufs.Rerror {
		t.Fatalf("Tclunk returned error: '%s'", rx.Ename)
	}

	// Create file /testdir/test.txt.  We can use dirfid, as it points to parent directory.
	tx.Reset()
	tx.Type = vufs.Tcreate
	tx.Fid = dirfid
	tx.Tag = tag
	tx.Name = "test.txt"
	tx.Mode = vufs.OREAD
	tx.Perm = vufs.Perm(0666)
	if err := vufs.WriteFcall(c, tx); err != nil {
		t.Fatalf("Tcreate failed: %v", err)
	}
	rx, err = vufs.ReadFcall(c)
	if err != nil {
		t.Fatalf("reading Rcreate failed: %v", err)
	}
	if rx.Type == vufs.Rerror {
		t.Errorf("Tcreate returned error: '%s'", rx.Ename)
	}

	// TODO(mbucc): test that file perm is 0600
}



// Create takes owner from request and group from parent directory.
// Root directory mode = 550 means no files in entire tree can be created.
// 700
// 570
// 557
