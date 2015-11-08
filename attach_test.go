/*
   Copyright (c) 2015, Mark Bucciarelli <mkbucc@gmail.com>
*/

package vufs_test

import (
	"github.com/mbucc/vufs"

	"net"
	"testing"
)

func setup_attach_test(t *testing.T) (*vufs.VuFs, net.Conn) {

	fs := vufs.New(".")
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

	return fs, c

}

func TestAttach(t *testing.T) {

	fs, c := setup_attach_test(t)
	if fs == nil || c == nil {
		return
	}
	defer fs.Stop()
	defer c.Close()

	tx := &vufs.Fcall{
		Type:    vufs.Tattach,
		Fid: 1,
		Tag:     1,
		Afid: vufs.NOFID,
		Uname: "mark",
		Aname: "/"}
	err := vufs.WriteFcall(c, tx)
	if err != nil {
		t.Fatalf("Tattach write failed: %v", err)
	}

	rx, err := vufs.ReadFcall(c)
	if err != nil {
		t.Errorf("Rattach read failed: %v", err)
	}
	if rx.Type == vufs.Rerror {
		t.Fatalf("Tattach returned error: '%s'", rx.Ename)
	}
	if rx.Type != vufs.Rattach {
		t.Errorf("bad message type, expected %d got %d", vufs.Rattach, rx.Type)
	}
	// Tag must be the same
	if rx.Tag != tx.Tag {
		t.Errorf("wrong tag, expected %d got %d", tx.Tag, rx.Tag)
	}
}
