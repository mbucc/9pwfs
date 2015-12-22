/*
   Copyright (c) 2015, Mark Bucciarelli <mkbucc@gmail.com>
*/

package vufs_test

import (
	"github.com/mbucc/vufs"
	"fmt"
	"io/ioutil"
	"os"
	"testing"
)


func TestRemoveWorks(t *testing.T) {

	rootdir, err := ioutil.TempDir("", "testremove")
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
	tf.walknames = []string{}
	tf.parentfid = config.rootfid
	tf.perm = vufs.Perm(0644)
	tf.mode = vufs.OWRITE
	for i := 1 ; i < 6 ; i++ {
		tf.name = fmt.Sprintf("testremove%d.txt", i)
		tf.newfid = uint32(1 + i)
		tf.create(t, c)
	}

	tx := new(vufs.Fcall)
	tx.Type = vufs.Topen
	tx.Mode = vufs.OREAD
	tx.Fid = config.rootfid
	writeTestFcall(t, c, tx)

	tx.Reset()
	tx.Type = vufs.Tread
	tx.Fid =config.rootfid
	tx.Tag = 1
	tx.Offset = 0
	tx.Count = 1000
	rx := writeTestFcall(t, c, tx)

	if rx.Count != 375 {
		t.Errorf("wrong count: expected 375 got %d", rx.Count)
	}

	tx.Reset()
	tx.Type = vufs.Tremove
	tx.Tag = 1
	for i := 1 ; i < 5 ; i++ {
		tx.Fid = uint32(1 + i)
		rx = writeTestFcall(t, c, tx)
	}

	tx.Reset()
	tx.Type = vufs.Tread
	tx.Fid =config.rootfid
	tx.Tag = 1
	tx.Offset = 0
	tx.Count = 1000
	rx = writeTestFcall(t, c, tx)

	if rx.Count != 75 {
		t.Errorf("wrong count: expected 75 got %d", rx.Count)
	}

	
}