/*
   Copyright (c) 2015, Mark Bucciarelli <mkbucc@gmail.com>
*/

package vufs

import (
	"os"
	"testing"
)



func TestLock(t *testing.T) {

	initfs(rootdir)
	defer os.RemoveAll(rootdir)

	conn := runserver(rootdir, ":5000")
	defer conn.Close()

	if conn == nil {
		t.Fail()
	}
	
}
