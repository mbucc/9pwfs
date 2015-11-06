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

func setup_stat_test(t *testing.T, fid uint32, rootdir string) (*vufs.VuFs, net.Conn) {

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

	tx = &vufs.Fcall{
		Type:  vufs.Tattach,
		Fid:   fid,
		Tag:   1,
		Afid:  vufs.NOFID,
		Uname: "mark",
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

func TestStat(t *testing.T) {

	rootdir, err := ioutil.TempDir("", "test")
	if err != nil {
		t.Fatalf("TempDir failed: %v", err)
	}
	defer os.RemoveAll(rootdir)

	fid := uint32(1)
	fs, c := setup_stat_test(t, fid, rootdir)
	if fs == nil || c == nil {
		return
	}
	defer fs.Stop()
	defer c.Close()

	tx := &vufs.Fcall{Type: vufs.Tstat, Fid: fid, Tag: 1}
	err = vufs.WriteFcall(c, tx)
	if err != nil {
		t.Fatalf("Tstat write failed: %v", err)
	}

	rx, err := vufs.ReadFcall(c)
	if err != nil {
		t.Errorf("Rstat read failed: %v", err)
	}
	if rx.Type == vufs.Rerror {
		t.Fatalf("attach returned error: '%s'", rx.Ename)
	}
	if rx.Type != vufs.Rstat {
		t.Errorf("bad message type, expected %d got %d", vufs.Rstat, rx.Type)
	}

	dir, err := vufs.UnmarshalDir(rx.Stat)
	if err != nil {
		t.Fatalf("UnmarshalDir failed: %v", rx.Ename)
	}
	if dir.Name != "/" {
		t.Errorf("wrong Name, expected '%s', got '%s'", "/", dir.Name)
	}

	if dir.Length != 0 {
		t.Errorf("directories, by convention, should have length 0")
	}

	if dir.Uid != vufs.DEFAULT_USER {
		t.Errorf("a new root directory should be owned by '%s', not '%s'",
			vufs.DEFAULT_USER, dir.Uid)
	}

	if dir.Gid != vufs.DEFAULT_USER {
		t.Errorf("a new root directory should have group by '%s', not '%s'",
			vufs.DEFAULT_USER, dir.Gid)
	}

	if dir.Gid != vufs.DEFAULT_USER {
		t.Errorf("a new root directory should have last modified user of '%s', not '%s'",
			vufs.DEFAULT_USER, dir.Muid)
	}

}
