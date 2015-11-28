/*
   Copyright (c) 2015, Mark Bucciarelli <mkbucc@gmail.com>
*/

package vufs_test

import (
	"github.com/mbucc/vufs"
	"bytes"
	"io/ioutil"
	"net"
	"os"
	"testing"
)

func writeTestFcall(t *testing.T, c net.Conn, tx *vufs.Fcall) (rx *vufs.Fcall) {
	err := vufs.WriteFcall(c, tx)
	if err != nil {
		t.Errorf("%s error: %v", tx, err)
	}
	rx, err = vufs.ReadFcall(c)
	if err != nil {
		t.Fatalf("can't read response to %s: %v", tx, err)
	}
	if rx.Type == vufs.Rerror {
		t.Fatalf("%s: got Rerror %s", tx, rx)
	}
	if rx.Type != tx.Type+1 {
		t.Fatalf("%s: bad response, expected %d got %d", tx, tx.Type+1, rx.Type)
	}
	return
}

func writeBadTestFcall(t *testing.T, c net.Conn, tx *vufs.Fcall) (rx *vufs.Fcall) {
	err := vufs.WriteFcall(c, tx)
	if err != nil {
		t.Errorf("%s error: %v", tx, err)
	}
	rx, err = vufs.ReadFcall(c)
	if err != nil {
		t.Fatalf("can't read response to %s: %v", tx, err)
	}
	if rx.Type != vufs.Rerror {
		t.Fatalf("%s: didn't get Rerror, got %s", tx, rx)
	}
	return
}

// Create the given file, owned by the given user.
func setupWriteTest(t *testing.T, rootfid, newfid uint32, rootdir, uid, filename string) (*vufs.VuFs, net.Conn) {

	// Start server and create connection.
	fs := vufs.New(rootdir)
	err := fs.Start("tcp", vufs.DEFAULTPORT)
	if err != nil {
		t.Fatalf("start failed: %v", err)
	}
	c, err := net.Dial("tcp", vufs.DEFAULTPORT)
	if err != nil {
		t.Fatalf("connection failed: %v", err)
	}
	//fs.Chatty(true)

	// Send version message.
	tx := &vufs.Fcall{
		Type:    vufs.Tversion,
		Tag:     vufs.NOTAG,
		Msize:   131072,
		Version: vufs.VERSION9P}
	rx := writeTestFcall(t, c, tx)
	if rx.Version != vufs.VERSION9P {
		t.Fatalf("bad version response, expected '%s' got '%s'", vufs.VERSION9P, rx.Version)
	}

	// Attach to root directory.
	tx.Reset()
	tx.Type = vufs.Tattach
	tx.Fid = rootfid
	tx.Tag = 1
	tx.Afid = vufs.NOFID
	tx.Uname = uid
	tx.Aname = "/"
	rx = writeTestFcall(t, c, tx)

	// Walk to root directory first so we don't lose the root fid.
	tx.Reset()
	tx.Type = vufs.Twalk
	tx.Fid = rootfid
	tx.Newfid = newfid
	tx.Tag = 1
	tx.Wname = []string{}
	rx = writeTestFcall(t, c, tx)

	// Create file.
	tx.Reset()
	tx.Type = vufs.Tcreate
	tx.Fid = newfid
	tx.Tag = 1
	tx.Name = filename
	tx.Mode = 0
	tx.Perm = vufs.Perm(0644)
	rx = writeTestFcall(t, c, tx)
	if rx.Qid.Path == 0 {
		t.Fatalf("Qid.Path was zero")
	}

	return fs, c

}

func TestFidMustExistForWriting(t *testing.T) {

	rootdir, err := ioutil.TempDir("", "testwrite")
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
	tf.name = "testwrite.txt"
	tf.walknames = []string{}
	tf.parentfid = config.rootfid
	tf.newfid = 2
	tf.perm = vufs.Perm(0644)
	tf.mode = vufs.OREAD
	tf.create(t, c)

	// Try and write to new file, using clunked fid.
	tx := new(vufs.Fcall)
	tx.Type = vufs.Twrite
	tx.Fid = tf.newfid
	tx.Tag = 1
	tx.Data = []byte("hello world")
	rx := writeBadTestFcall(t, c, tx)
	if rx.Ename != "not opened for writing" {
		t.Fatalf("bad error, expected '%s' got '%s'", "not opened for writing", rx.Ename)
	}

}

func TestWriteWorks(t *testing.T) {

	rootdir, err := ioutil.TempDir("", "testwrite")
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
	tf.name = "testwrite.txt"
	tf.walknames = []string{}
	tf.parentfid = config.rootfid
	tf.newfid = 2
	tf.perm = vufs.Perm(0644)
	tf.mode = vufs.OWRITE
	tf.create(t, c)

	data := []byte("Hello World!")

	tx := new(vufs.Fcall)
	tx.Type = vufs.Twrite
	tx.Fid = tf.newfid
	tx.Tag = 1
	tx.Data = data
	writeTestFcall(t, c, tx)

	tx.Reset()
	tx.Type = vufs.Tread
	tx.Fid = tf.newfid
	tx.Tag = 1
	tx.Offset = 0
	tx.Count = 50
	rx := writeTestFcall(t, c, tx)

	if !bytes.Equal(rx.Data, data) {
		t.Errorf("bad data: expected '%s', got '%s'", data, rx.Data)
	}

}

