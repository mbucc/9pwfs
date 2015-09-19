/*
   Copyright (c) 2015, Mark Bucciarelli <mkbucc@gmail.com>
*/

package vufs

import (
	"bytes"
	"io/ioutil"
	"os"
	"testing"

	"9fans.net/go/plan9"
	"9fans.net/go/plan9/client"
)

import "fmt"

const (
	port               = ":5000"
	messageSizeInBytes = 8192
	rootdir            = "./tmpfs"
)

type fileTest struct {
	path    string
	mode    os.FileMode
	op      string
	user    string
	allowed bool
}

func (ft fileTest) String() string {
	v := "cannot"
	if ft.allowed {
		v = "can"
	}
	return fmt.Sprintf("%s %s %s '%s' %s",  ft.user, v, ft.op, ft.path, ft.mode)
}

var expectedContents = map[string]string {
	"/": ".uidgid, adm, larry-moe.txt, moe-moe.txt",
	"/moe-moe.txt": "whatever",
	"/larry-moe.txt": "whatever",

}

// Initialize file system as:
//
//          /
//           |
//           +-- adm/            --rwx------ adm adm
//           |       |
//           |       +-- users     --rw------- adm adm
//           |
//           +-- .uidgid          --rw-rw---- adm moe
//           |
//           +-- moe-moe.txt     --rw-rw-r-- moe moe
//           |
//           +-- larry-moe.txt     --rw-rw-r-- larry moe
//
//         Notes:
//
//          a.    Users shown are virtual ones, not ones on disk.
//
//          b.    If no ownership specified (in .uidgid), it defaults to adm adm.
//
//
func initfs(rootdir string) {

	// Begin anew.
	err := os.RemoveAll(rootdir)
	if err != nil {
		msg := fmt.Sprintf("os.RemoveAll(%s) failed: %v\n", rootdir, err)
		panic(msg)
	}
	err = os.Mkdir(rootdir, 0755)
	if err != nil {
		msg := fmt.Sprintf("os.Mkdir(%s, 0755) failed: %v\n", rootdir, err)
		panic(msg)
	}

	// Write users and their ID's.
	err = os.Mkdir(rootdir+"/adm", 0700)
	if err != nil {
		msg := fmt.Sprintf("os.Mkdir(%s, 0700) failed: %v\n", rootdir+"/adm", err)
		panic(msg)
	}
	err = ioutil.WriteFile(rootdir+"/adm/users", 
			[]byte("1:adm:adm\n2:larry:larry\n3:moe:moe\n4:curly:curly\n"), 0600)
	if err != nil {
		msg := fmt.Sprintf("ioutil.WriteFile(%s, 0600) failed: %v\n", 
				rootdir+"/adm/users", err)
		panic(msg)
	}

	// Create files for testing.
	err = ioutil.WriteFile(rootdir+"/moe-moe.txt", []byte("whatever"), 0664)
	if err != nil {
		msg := fmt.Sprintf("ioutil.WriteFile(%s, 0664) failed: %v\n", 
				rootdir+"/moe-moe.txt", err)
		panic(msg)
	}
	err = ioutil.WriteFile(rootdir+"/larry-moe.txt", []byte("whatever"), 0664)
	if err != nil {
		msg := fmt.Sprintf("ioutil.WriteFile(%s, 0664) failed: %v\n", 
				rootdir+"/moe-moe.txt", err)
		panic(msg)
	}

	// Record file ownership.
	err = ioutil.WriteFile(rootdir+"/"+uidgidFile, 
			[]byte("moe-moe.txt:3:3\nlarry-moe.txt:2:3\n"), 0600)
	if err != nil {
		msg := fmt.Sprintf("ioutil.WriteFile(%s, 0600) failed: %v\n", 
			rootdir+"/moe-moe.txt", err)
		panic(msg)
	}
}

func runserver(rootdir, port string) *client.Conn {

	var err error
	fs := New(rootdir)
	fs.Id = "vufs"
	fs.Upool, err = NewVusers(rootdir)
	if err != nil {
		panic(err)
	}
	//fs.Debuglevel = 1

	fs.Start(fs)

	go func() {
		err = fs.StartNetListener("tcp", port)
		if err != nil {
			panic(err)
		}
	}()

	// Make sure runserver is listening before returning.
	var conn *client.Conn
	for i := 0; i < 16; i++ {
		if conn, err = client.Dial("tcp", port); err == nil {
			break
		}

	}

	if err != nil {
		panic("couldn't connect to runserver after 15 tries: " + err.Error())
	}

	return conn
}

func readDir(fid *client.Fid) ([]byte, error) {

	d, err := fid.Dirreadall()
	if err != nil {
		return nil, err
	}

	var buf  bytes.Buffer
	for i, dd := range d {
		if i > 0 {
			buf.Write([]byte(", "))
		}
		buf.Write([]byte(dd.Name))
	}

	return buf.Bytes(), nil
}

func read(conn *client.Conn, username, filepath string) (string, error) {

	fsys, err := conn.Attach(nil, username, "/")

	if err != nil {
		return "", err
	}

	fid, err := fsys.Open(filepath, plan9.OREAD)

	if err != nil {
		return "", err
	}
	defer fid.Close()

	var buf []byte
	if fid.Qid().Type & plan9.QTDIR != 0 {
		buf, err = readDir(fid)
	} else {
		buf, err = ioutil.ReadAll(fid)
	}

	return string(buf), err

}

// Return bytes written, resulting content, and any error.
func write(conn *client.Conn, username, filepath, contents string) (int, string, error) {

	fsys, err := conn.Attach(nil, username, "/")

	if err != nil {
		return 0, "", err
	}

	fid, err := fsys.Open(filepath, plan9.OWRITE)

	if err != nil {
		return 0, "", err
	}

	defer fid.Close()

	n, err := fid.Write([]byte("whom"))

	if err != nil {
		return n, "", err
	}

	newcontents, err := ioutil.ReadFile(rootdir + "/" + filepath)

	return n, string(newcontents), err

}


func TestFiles(t *testing.T) {

	initfs(rootdir)

	conn := runserver(rootdir, port)

	for _, tt := range fileTests {

		err := os.Chmod(rootdir+tt.path, tt.mode)

		if err != nil {
			t.Errorf("%+v: chmod failed: %v\n", tt, err)
		}

		switch tt.op {

		default:
			t.Errorf("Unsupported operation %s in fileTest = %s\n", tt.op, tt)

		case "write":
			n, newcontents, err := write(conn, tt.user, tt.path, "whom")
			if tt.allowed {
				if err != nil {
					t.Errorf("%s: %v\n", tt, err)
				} else if n != 4 {
					t.Errorf("%s: exp = 4, act = %d\n", tt, n)
				} else if newcontents != "whomever" {
					t.Errorf("%s: exp = 'whomever', act = '%s'\n", tt, newcontents)
				} else {
					// EMPTY --- test passed.
				}
			
			} else {
				if err == nil {
					t.Errorf("%s: was allowed\n", tt)
				} else {
					// EMPTY --- test passed.
				}
			}

		case "read":
			contents, err := read(conn, tt.user, tt.path)
			if tt.allowed {
				if err != nil {
					t.Errorf("%s: %v\n", tt, err)
				} else if contents != expectedContents[tt.path] {
					t.Errorf("%s: exp = '%s', act = '%s'\n", 
							expectedContents[tt.path], contents)
				} else {
					// EMPTY --- test passed.
				}
			} else {
				if err == nil {
					t.Errorf("%s: was allowed\n", tt)
				} else {
					// EMPTY --- test passed.
				}
			}
		}
	}
}


var fileTests []fileTest = []fileTest {

	// Root directory
	{"/", 0700, "read", "adm", true},
	{"/", 0700, "read", "moe", false},
	{"/", 0700, "read", "curly", false},

	{"/", 0750, "read", "adm", true},
	{"/", 0750, "read", "moe", false},
	{"/", 0750, "read", "curly", false},

	{"/", 0755, "read", "adm", true},
	{"/", 0755, "read", "moe", true},
	{"/", 0755, "read", "curly", true},

	// Read file with same user and group (moe)
	{"/moe-moe.txt", 0600, "read", "adm", false},
	{"/moe-moe.txt", 0600, "read", "moe", true},
	{"/moe-moe.txt", 0600, "read", "curly", false},

	{"/moe-moe.txt", 0440, "read", "adm", false},
	{"/moe-moe.txt", 0440, "read", "moe", true},
	{"/moe-moe.txt", 0440, "read", "curly", false},

	{"/moe-moe.txt", 0444, "read", "adm", true},
	{"/moe-moe.txt", 0444, "read", "moe", true},
	{"/moe-moe.txt", 0444, "read", "curly", true},

	// Read file with different user (larry) and group (moe)
	{"/larry-moe.txt", 0600, "read", "adm", false},
	{"/larry-moe.txt", 0600, "read", "moe", false},
	{"/larry-moe.txt", 0600, "read", "larry", true},
	{"/larry-moe.txt", 0600, "read", "curly", false},

	{"/larry-moe.txt", 0440, "read", "adm", false},
	{"/larry-moe.txt", 0440, "read", "moe", true},
	{"/larry-moe.txt", 0440, "read", "larry", true},
	{"/larry-moe.txt", 0440, "read", "curly", false},

	{"/larry-moe.txt", 0444, "read", "adm", true},
	{"/larry-moe.txt", 0444, "read", "moe", true},
	{"/larry-moe.txt", 0444, "read", "larry", true},
	{"/larry-moe.txt", 0444, "read", "curly", true},

	// Write file with same user and group (moe)
	{"/moe-moe.txt", 0400, "write", "moe", false},
	{"/moe-moe.txt", 0440, "write", "moe", false},
	{"/moe-moe.txt", 0444, "write", "moe", false},
	{"/moe-moe.txt", 0200, "write", "moe", false},
	{"/moe-moe.txt", 0000, "write", "moe", false},

	{"/moe-moe.txt", 0600, "write", "moe", true},
	{"/moe-moe.txt", 0600, "write", "adm", false},
	{"/moe-moe.txt", 0600, "write", "curly", false},

	{"/moe-moe.txt", 0660, "write", "adm", false},
	{"/moe-moe.txt", 0660, "write", "curly", false},
	{"/moe-moe.txt", 0666, "write", "adm", true},
	{"/moe-moe.txt", 0666, "write", "curly", true},

	// Write file with different user (larry) and group (moe)
	{"/larry-moe.txt", 0600, "write", "adm", false},
	{"/larry-moe.txt", 0600, "write", "moe", false},
	{"/larry-moe.txt", 0600, "write", "larry", true},
	{"/larry-moe.txt", 0600, "write", "curly", false},

	{"/larry-moe.txt", 0660, "write", "adm", false},
	{"/larry-moe.txt", 0660, "write", "moe", true},
	{"/larry-moe.txt", 0660, "write", "larry", true},
	{"/larry-moe.txt", 0660, "write", "curly", false},
}
