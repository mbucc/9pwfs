/*
   Copyright (c) 2015, Mark Bucciarelli <mkbucc@gmail.com>
*/

package vufs_test

import (
	"9fans.net/go/plan9/client"
	"github.com/mbucc/vufs"

	"net"
	"testing"
)

func TestVersion(t *testing.T) {

	fs := vufs.New(".")
	//fs.Chatty(true)
	err := fs.Start("tcp", vufs.DEFAULTPORT)
	if err != nil {
		t.Fatalf("start failed: %v", err)
	}
	defer fs.Stop()

	c, err := client.Dial("tcp", vufs.DEFAULTPORT)
	if err != nil {
		t.Errorf("connection failed: %v", err)
	}
	if c == nil {
		t.Errorf("client was nil.")
	}
}

func TestUnknownVersion(t *testing.T) {

	fs := vufs.New(".")
	err := fs.Start("tcp", vufs.DEFAULTPORT)
	if err != nil {
		t.Fatalf("start failed: %v", err)
	}
	defer fs.Stop()

	c, err := net.Dial("tcp", vufs.DEFAULTPORT)
	if err != nil {
		t.Errorf("connection failed: %v", err)
	}
	defer c.Close()

	tx := &vufs.Fcall{
		Type:    vufs.Tversion,
		Tag:     vufs.NOTAG,
		Msize:   131072,
		Version: "ABC123"}
	err = vufs.WriteFcall(c, tx)
	if err != nil {
		t.Errorf("connection write failed: %v", err)
	}
	rx, err := vufs.ReadFcall(c)
	if err != nil {
		t.Errorf("connection read failed: %v", err)
	}
	if rx.Type != vufs.Rversion {
		t.Errorf("bad message type, expected %d got %d", vufs.Rversion, rx.Type)
	}
	if rx.Version != "unknown" {
		t.Errorf("bad version response, expected 'unknown' got '%s'", rx.Version)
	}
}



func TestVersionExtension(t *testing.T) {

	fs := vufs.New(".")
	err := fs.Start("tcp", vufs.DEFAULTPORT)
	if err != nil {
		t.Fatalf("start failed: %v", err)
	}
	defer fs.Stop()

	c, err := net.Dial("tcp", vufs.DEFAULTPORT)
	if err != nil {
		t.Errorf("connection failed: %v", err)
	}
	defer c.Close()

	tx := &vufs.Fcall{
		Type:    vufs.Tversion,
		Tag:     vufs.NOTAG,
		Msize:   131072,
		Version: "9P2000.u"}
	err = vufs.WriteFcall(c, tx)
	if err != nil {
		t.Errorf("connection write failed: %v", err)
	}
	rx, err := vufs.ReadFcall(c)
	if err != nil {
		t.Errorf("connection read failed: %v", err)
	}
	if rx.Type != vufs.Rversion {
		t.Errorf("bad message type, expected %d got %d", vufs.Rversion, rx.Type)
	}
	if rx.Version != "9P2000" {
		t.Errorf("bad version response, expected '9P2000' got '%s'", rx.Version)
	}
}


func TestBigMessageSizeClamped(t *testing.T) {

	fs := vufs.New(".")
	err := fs.Start("tcp", vufs.DEFAULTPORT)
	if err != nil {
		t.Fatalf("start failed: %v", err)
	}
	defer fs.Stop()

	c, err := net.Dial("tcp", vufs.DEFAULTPORT)
	if err != nil {
		t.Errorf("connection failed: %v", err)
	}
	defer c.Close()

	tx := &vufs.Fcall{
		Type:    vufs.Tversion,
		Tag:     vufs.NOTAG,
		Msize:   vufs.MAX_MSIZE + 100,
		Version: vufs.VERSION9P}
	err = vufs.WriteFcall(c, tx)
	if err != nil {
		t.Errorf("connection write failed: %v", err)
	}
	rx, err := vufs.ReadFcall(c)
	if err != nil {
		t.Errorf("connection read failed: %v", err)
	}

	if rx.Msize != vufs.MAX_MSIZE {
		t.Errorf("bad msize, expected %d got %d", vufs.MAX_MSIZE, rx.Msize)
	}
}


