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
	allowed  bool
	user     string
	op       string
	mode     os.FileMode
	path     string
	keepState bool
}

func (ft optest) String() string {
	v := "cannot"
	if ft.allowed {
		v = "can"
	}
	return fmt.Sprintf("%s %s %s '%s' %s", ft.user, v, ft.op, ft.path, ft.mode)
}

// initialFile is a file on disk used for testing.
type initialFile struct {
	path     string
	contents string
	mode     os.FileMode
}

func (f initialFile) String() string {
	return fmt.Sprintf("%s %s", f.path, f.mode)
}

var initialFiles = map[string]initialFile{
	"/":     {"/", ".uidgid, adm, larry-moe.txt, moe-moe.txt", 0775},
	"/adm/": {"/adm/", "", 0775},
	"/adm/users": {"/adm/users",
		"1:adm:adm\n2:larry:larry\n3:moe:moe\n4:curly:curly\n",
		0600},
	"/moe-moe.txt":   {"/moe-moe.txt", "whatever", 0664},
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

	var buf bytes.Buffer
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
	if fid.Qid().Type&plan9.QTDIR != 0 {
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
func usergroup(conn *client.Conn, filepath, user string) (string, string, error) {

	fsys, err := conn.Attach(nil, user, "/")

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

		if !tt.keepState {
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
				user, group, err := usergroup(conn, tt.path, tt.user)
				if err != nil {
					t.Errorf("%s: usergroup('%s'): %s\n", tt, tt.path, err)
				} else {
					if user != tt.user {
						t.Errorf("%s: wrong user, got '%s', expected '%s'\n",
							tt, user, tt.user)
					}

					if group != tt.user {
						t.Errorf("%s: wrong group, got '%s', expected '%s'\n",
							tt, user, tt.user)
					}
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
					t.Errorf("%s: exp = '%s', act = '%s'\n", f.contents, contents)
				}
			} else {
				if err == nil {
					t.Errorf("%s: was allowed\n", tt)
				}
			}
		}
	}
}

var optests []optest = []optest{

	// Root directory
	{true, "adm", "read", 0700, "/", false},
	{false, "moe", "read", 0700, "/", false},
	{false, "curly", "read", 0700, "/", false},

	{true, "adm", "read", 0750, "/", false},
	{false, "moe", "read", 0750, "/", false},
	{false, "curly", "read", 0750, "/", false},

	{true, "adm", "read", 0755, "/", false},
	{true, "moe", "read", 0755, "/", false},
	{true, "curly", "read", 0755, "/", false},

	// Read file with same user and group (moe)
	{false, "adm", "read", 0600, "/moe-moe.txt", false},
	{true, "moe", "read", 0600, "/moe-moe.txt", false},
	{false, "curly", "read", 0600, "/moe-moe.txt", false},

	{false, "adm", "read", 0440, "/moe-moe.txt", false},
	{true, "moe", "read", 0440, "/moe-moe.txt", false},
	{false, "curly", "read", 0440, "/moe-moe.txt", false},

	{true, "adm", "read", 0444, "/moe-moe.txt", false},
	{true, "moe", "read", 0444, "/moe-moe.txt", false},
	{true, "curly", "read", 0444, "/moe-moe.txt", false},

	// Read file with different user (larry) and group (moe)
	{false, "adm", "read", 0600, "/larry-moe.txt", false},
	{false, "moe", "read", 0600, "/larry-moe.txt", false},
	{true, "larry", "read", 0600, "/larry-moe.txt", false},
	{false, "curly", "read", 0600, "/larry-moe.txt", false},

	{false, "adm", "read", 0440, "/larry-moe.txt", false},
	{true, "moe", "read", 0440, "/larry-moe.txt", false},
	{true, "larry", "read", 0440, "/larry-moe.txt", false},
	{false, "curly", "read", 0440, "/larry-moe.txt", false},

	{true, "adm", "read", 0444, "/larry-moe.txt", false},
	{true, "moe", "read", 0444, "/larry-moe.txt", false},
	{true, "larry", "read", 0444, "/larry-moe.txt", false},
	{true, "curly", "read", 0444, "/larry-moe.txt", false},

	// Write file with same user and group (moe)
	{false, "moe", "write", 0400, "/moe-moe.txt", false},
	{false, "moe", "write", 0440, "/moe-moe.txt", false},
	{false, "moe", "write", 0444, "/moe-moe.txt", false},
	{false, "moe", "write", 0200, "/moe-moe.txt", false},
	{false, "moe", "write", 0000, "/moe-moe.txt", false},

	{true, "moe", "write", 0600, "/moe-moe.txt", false},
	{false, "adm", "write", 0600, "/moe-moe.txt", false},
	{false, "curly", "write", 0600, "/moe-moe.txt", false},

	{false, "adm", "write", 0660, "/moe-moe.txt", false},
	{false, "curly", "write", 0660, "/moe-moe.txt", false},
	{true, "adm", "write", 0666, "/moe-moe.txt", false},
	{true, "curly", "write", 0666, "/moe-moe.txt", false},

	// Write file with different user (larry) and group (moe)
	{false, "adm", "write", 0600, "/larry-moe.txt", false},
	{false, "moe", "write", 0600, "/larry-moe.txt", false},
	{true, "larry", "write", 0600, "/larry-moe.txt", false},
	{false, "curly", "write", 0600, "/larry-moe.txt", false},

	{false, "adm", "write", 0660, "/larry-moe.txt", false},
	{true, "moe", "write", 0660, "/larry-moe.txt", false},
	{true, "larry", "write", 0660, "/larry-moe.txt", false},
	{false, "curly", "write", 0660, "/larry-moe.txt", false},

	// Create files.
	{true, "moe", "create", os.ModeDir + 0755, "/books", false},
	{true, "larry", "create", os.ModeDir + 0700, "/books/larry", true},
	{true, "larry", "create", 0600, "/books/larry/draft", true},
	{false, "moe", "create", 0600, "/books/larry/moe-draft", true},

	{true, "adm", "create", os.ModeDir + 0755, "/books", false},
}
