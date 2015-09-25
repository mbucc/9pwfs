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

// optest is a test of one file operation; for example, "read contents of / as user moe".
type optest struct {
	path    string
	mode    os.FileMode
	op      string
	user    string
	allowed bool
	keepLast bool
}

func (ft optest) String() string {
	v := "cannot"
	if ft.allowed {
		v = "can"
	}
	return fmt.Sprintf("%s %s %s '%s' %s",  ft.user, v, ft.op, ft.path, ft.mode)
}

// initialFile is a file on disk used for testing.
type initialFile struct {
	path string
	contents	string
	mode	os.FileMode
}

func (f initialFile) String() string {
	return fmt.Sprintf("%s %s",  f.path, f.mode)
}

var initialFiles = map[string]initialFile {
	"/": {"/",  ".uidgid, adm, larry-moe.txt, moe-moe.txt", 0775},
	"/adm/": {"/adm/", "", 0775},
	"/adm/users": {"/adm/users", 
			"1:adm:adm\n2:larry:larry\n3:moe:moe\n4:curly:curly\n", 
			0600},
	"/moe-moe.txt": {"/moe-moe.txt", "whatever",  0664},
	"/larry-moe.txt": {"/larry-moe.txt", "whatever", 0664},
	"/" + uidgidFile: {"/" + uidgidFile, "moe-moe.txt:3:3\nlarry-moe.txt:2:3\n", 0600},
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

	// Remove entire file system and recreate root folder
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

	// Create initial filesystem.  Directories first
	for path, f := range initialFiles {
		if path[len(path)-1] != '/' {
			continue
		}
		p := rootdir + f.path
		err = os.MkdirAll(p, f.mode)
		if err != nil {
			panic(fmt.Sprintf("os.MkdirAll(%s, %s) failed: %v\n", p, f.mode, err))
		}
	}

	// Now that directories are created, do files.
	for path, f := range initialFiles {
		if path[len(path)-1] == '/' {
			continue
		}
		p := rootdir + f.path
	
		err = ioutil.WriteFile(p, []byte(f.contents), f.mode)
		if err != nil {
			panic(fmt.Sprintf("ioutil.WriteFile(%s, %s) failed: %v\n", p, f.mode, err))
		}
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

// Create file or directory.
func create(conn *client.Conn, username, filepath string, mode os.FileMode) error {

	fsys, err := conn.Attach(nil, username, "/")

	if err != nil {
		return err
	}

	fid, err := fsys.Create(filepath, plan9.OREAD, plan9.Perm(mode))

	if err != nil {
		return err
	}

	fid.Close()

	return nil
}

// Return owner and group of given file.
func usergroup(conn *client.Conn, filepath string) (string, string, error) {

	fsys, err := conn.Attach(nil, "adm", "/")

	if err != nil {
		return "", "", err
	}

	dir, err := fsys.Stat(filepath)

	if err != nil {
		return "", "", err
	}

	return dir.Uid, dir.Gid, nil
}



func TestFiles(t *testing.T) {


	conn := runserver(rootdir, port)

	for _, tt := range optests {

		if !tt.keepLast {
			initfs(rootdir)
		}

		switch tt.op {

		default:
			t.Errorf("Unsupported operation %s in optest = %s\n", tt.op, tt)

		case "create":
			err := create(conn, tt.user, tt.path, tt.mode)
			if tt.allowed {
				if err != nil {
					t.Errorf("%s: %v\n", tt, err)
				} 
				user, group, err := usergroup(conn, tt.path)
				if err != nil {
					t.Errorf("%s: couldn't stat file, got %s\n", tt, err)
				} 
		
				if user != tt.user {
					t.Errorf("%s: wrong user, got '%s', expected '%s'\n", 
							tt, user, tt.user)
				}

				if group != tt.user {
					t.Errorf("%s: wrong group, got '%s', expected '%s'\n", 
							tt, user, tt.user)
				}

			} else {
				if err == nil {
					t.Errorf("%s: was allowed\n", tt)
				}
			}

		case "write":
			err := os.Chmod(rootdir+tt.path, tt.mode)
			if err != nil {
				t.Errorf("%+v: chmod failed: %v\n", tt, err)
			}

			n, newcontents, err := write(conn, tt.user, tt.path, "whom")
			if tt.allowed {
				if err != nil {
					t.Errorf("%s: %v\n", tt, err)
				} 

				if n != 4 {
					t.Errorf("%s: exp = 4, act = %d\n", tt, n)
				} 

				if newcontents != "whomever" {
					t.Errorf("%s: exp = 'whomever', act = '%s'\n", tt, newcontents)
				}
			
			} else {
				if err == nil {
					t.Errorf("%s: was allowed\n", tt)
				}
			}

		case "read":
			err := os.Chmod(rootdir+tt.path, tt.mode)
			if err != nil {
				t.Errorf("%+v: chmod failed: %v\n", tt, err)
			}
			contents, err := read(conn, tt.user, tt.path)
			if tt.allowed {
				if err != nil {
					t.Errorf("%s: %v\n", tt, err)
				}
				f, found := initialFiles[tt.path]
				if !found {
					t.Errorf("%s: not found in initialFiles\n", tt)
				}
				if contents != f.contents {
					t.Errorf("%s: exp = '%s', act = '%s'\n", 	f.contents, contents)
				}
			} else {
				if err == nil {
					t.Errorf("%s: was allowed\n", tt)
				}
			}
		}
	}
}


var optests []optest = []optest {

	// Root directory
	{"/", 0700, "read", "adm", true, false},
	{"/", 0700, "read", "moe", false, false},
	{"/", 0700, "read", "curly", false, false},

	{"/", 0750, "read", "adm", true, false},
	{"/", 0750, "read", "moe", false, false},
	{"/", 0750, "read", "curly", false, false},

	{"/", 0755, "read", "adm", true, false},
	{"/", 0755, "read", "moe", true, false},
	{"/", 0755, "read", "curly", true, false},

	// Read file with same user and group (moe)
	{"/moe-moe.txt", 0600, "read", "adm", false, false},
	{"/moe-moe.txt", 0600, "read", "moe", true, false},
	{"/moe-moe.txt", 0600, "read", "curly", false, false},

	{"/moe-moe.txt", 0440, "read", "adm", false, false},
	{"/moe-moe.txt", 0440, "read", "moe", true, false},
	{"/moe-moe.txt", 0440, "read", "curly", false, false},

	{"/moe-moe.txt", 0444, "read", "adm", true, false},
	{"/moe-moe.txt", 0444, "read", "moe", true, false},
	{"/moe-moe.txt", 0444, "read", "curly", true, false},

	// Read file with different user (larry) and group (moe)
	{"/larry-moe.txt", 0600, "read", "adm", false, false},
	{"/larry-moe.txt", 0600, "read", "moe", false, false},
	{"/larry-moe.txt", 0600, "read", "larry", true, false},
	{"/larry-moe.txt", 0600, "read", "curly", false, false},

	{"/larry-moe.txt", 0440, "read", "adm", false, false},
	{"/larry-moe.txt", 0440, "read", "moe", true, false},
	{"/larry-moe.txt", 0440, "read", "larry", true, false},
	{"/larry-moe.txt", 0440, "read", "curly", false, false},

	{"/larry-moe.txt", 0444, "read", "adm", true, false},
	{"/larry-moe.txt", 0444, "read", "moe", true, false},
	{"/larry-moe.txt", 0444, "read", "larry", true, false},
	{"/larry-moe.txt", 0444, "read", "curly", true, false},

	// Write file with same user and group (moe)
	{"/moe-moe.txt", 0400, "write", "moe", false, false},
	{"/moe-moe.txt", 0440, "write", "moe", false, false},
	{"/moe-moe.txt", 0444, "write", "moe", false, false},
	{"/moe-moe.txt", 0200, "write", "moe", false, false},
	{"/moe-moe.txt", 0000, "write", "moe", false, false},

	{"/moe-moe.txt", 0600, "write", "moe", true, false},
	{"/moe-moe.txt", 0600, "write", "adm", false, false},
	{"/moe-moe.txt", 0600, "write", "curly", false, false},

	{"/moe-moe.txt", 0660, "write", "adm", false, false},
	{"/moe-moe.txt", 0660, "write", "curly", false, false},
	{"/moe-moe.txt", 0666, "write", "adm", true, false},
	{"/moe-moe.txt", 0666, "write", "curly", true, false},

	// Write file with different user (larry) and group (moe)
	{"/larry-moe.txt", 0600, "write", "adm", false, false},
	{"/larry-moe.txt", 0600, "write", "moe", false, false},
	{"/larry-moe.txt", 0600, "write", "larry", true, false},
	{"/larry-moe.txt", 0600, "write", "curly", false, false},

	{"/larry-moe.txt", 0660, "write", "adm", false, false},
	{"/larry-moe.txt", 0660, "write", "moe", true, false},
	{"/larry-moe.txt", 0660, "write", "larry", true, false},
	{"/larry-moe.txt", 0660, "write", "curly", false, false},

	// Create files.
	{"/books", os.ModeDir + 0755, "create", "moe", true, false},
	{"/books/larry", os.ModeDir + 0700, "create", "larry", true,  true},
/*
	{"/books/larry/draft", 0600, "create", "larry", true,  true},
	{"/books/larry/moe-draft", 0600, "create", "moe", false,  true},
	{"/books", os.ModeDir + 0755, "create", "adm", true,false},
*/

}
