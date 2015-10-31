/*
   Copyright (c) 2015, Mark Bucciarelli <mkbucc@gmail.com>
*/

package vufs_test

import (
	"bytes"
	"io/ioutil"
	"net"
	"os"
	"testing"
	"time"
	"github.com/mbucc/vufs"

	"9fans.net/go/plan9"
	"9fans.net/go/plan9/client"
)

import "fmt"

const (
	port               = ":5000"
	messageSizeInBytes = 8192
)

// optest is a test of one file operation; for example, "read contents of / as user moe".
type optest struct {
	allowed   bool
	user      string
	op        string
	mode      os.FileMode
	path      string
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

// 0.05 milliseconds.
func BenchmarkAttach(b *testing.B) {

	conn := runserver(rootdir, port)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		conn.Attach(nil, "adm", "/")
	}
}

// 0.18 milliseconds.
func BenchmarkOpenClose(b *testing.B) {

	conn := runserver(rootdir, port)
	fsys, _ := conn.Attach(nil, "adm", "/")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fid, _ := fsys.Open("/", plan9.OREAD)
		fid.Close()
	}
}

// 0.003 milliseconds (~60X faster than vufs).
func BenchmarkOsOpenClose(b *testing.B) {

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fid, _ := os.Open(".")
		fid.Close()
	}
}

// 0.06 milliseconds.
func BenchmarkReadDir(b *testing.B) {

	conn := runserver(rootdir, port)
	fsys, _ := conn.Attach(nil, "adm", "/")
	fid, _ := fsys.Open("/", plan9.OREAD)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fid.Dirread()
	}
}

var initialFiles = map[string]initialFile{
	"/":     {"/", ".uidgid, adm, larry-moe.txt, moe-moe.txt", 0775},
	"/adm/": {"/adm/", "", 0775},
	"/adm/users": {"/adm/users",
		"1:adm:adm\n2:larry:larry\n3:moe:moe\n4:curly:curly\n",
		0600},
	"/moe-moe.txt":   {"/moe-moe.txt", "whatever", 0664},
	"/larry-moe.txt": {"/larry-moe.txt", "whatever", 0664},
	//"/" + uidgidFile: {"/" + uidgidFile, "moe-moe.txt:3:3\nlarry-moe.txt:2:3\n", 0600},
}

// Initialize file system as:
//
//          /            --rwxrwxr-x adm adm
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

	isdir := func(s string) bool { return s[len(s)-1] == '/' }

	// Create initial filesystem.  Directories first
	for path, f := range initialFiles {
		if isdir(path) {
			p := rootdir + f.path
			err = os.MkdirAll(p, f.mode)
			if err != nil {
				panic(fmt.Sprintf("os.MkdirAll(%s, %s) failed: %v\n", p, f.mode, err))
			}
		}
	}

	// Now that directories are created, do files.
	for path, f := range initialFiles {
		if !isdir(path) {
			p := rootdir + f.path

			err = ioutil.WriteFile(p, []byte(f.contents), f.mode)
			if err != nil {
				panic(fmt.Sprintf("ioutil.WriteFile(%s, %s) failed: %v\n", p, f.mode, err))
			}
		}

	}
}


var testserver net.Listener
var started bool

// Start up vufs file server.  If it is already running, stop it first.
func runserver(rootdir, port string) *client.Conn {

	initfs(rootdir)

	var err error
	fs := vufs.New(rootdir)
	fs.Id = "vufs"
	fs.Upool, err = vufs.NewVusers(rootdir)
	if err != nil {
		panic(err)
	}
	//fs.Debuglevel = 1

	fs.Start(fs)

	if started {
		fmt.Println("stopping testserver")
		err = testserver.Close()
		if err != nil {
			panic(err)
		}
		time.Sleep(250 * time.Millisecond)
	} else {
		fmt.Println("testserver not running")
	}

	fmt.Println("starting testserver")
	testserver, err = net.Listen("tcp", port)
	if err != nil {
		panic("can't start server: " + err.Error())
	}
	started = true

	go func() {
		// Give last go routine time to stop listening.
		for i := 0; i < 10; i++ {
			if err = fs.StartListener(testserver); err != nil {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
	}()

	// Make sure runserver is listening before returning.
	var conn *client.Conn
	for i := 0; i < 15; i++ {
		if conn, err = client.Dial("tcp", port); err == nil {
			break
		}
	}

	if err != nil {
		panic("filesystem server didn't start" + err.Error())
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

// Delete file or directory
func delete(conn *client.Conn, username, filepath string) error {

	fsys, err := conn.Attach(nil, username, "/")

	if err != nil {
		return err
	}

	err = fsys.Remove(filepath)

	if err != nil {
		return err
	}

	return nil

}

// Change file owner.
func chown(conn *client.Conn, username, filepath string) error {

	fsys, err := conn.Attach(nil, username, "/")
	if err != nil {
		return err
	}

	d := &plan9.Dir{}
	d.Null()
	d.Uid = username
	err = fsys.Wstat(filepath, d)

	if err != nil {
		return err
	}

	return nil

}

// Change file group.
func chgrp(conn *client.Conn, username, filepath string) error {

	fsys, err := conn.Attach(nil, username, "/")
	if err != nil {
		return err
	}

	d := &plan9.Dir{}
	d.Null()
	d.Gid = username
	err = fsys.Wstat(filepath, d)

	if err != nil {
		return err
	}

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

// Change directory owner.
func TestWstat(t *testing.T) {
	conn := runserver(rootdir, port)

	err := create(conn, "adm", "/books", os.ModeDir+0755)
	if err != nil {
		t.Errorf("adm could not create /books")
	}
	err = chown(conn, "moe", "/books")
	if err != nil {
		t.Errorf("adm could not chown to moe")
	}
	err = chgrp(conn, "moe", "/books")
	if err != nil {
		t.Errorf("adm could not chown to moe")
	}
	user, group, err := usergroup(conn, "/books", "adm")
	if err != nil {
		t.Errorf("usergroup('/books'): %s\n", err)
	}
	if user != "moe" {
		t.Errorf("wrong user, got '%s', expected 'moe'\n", user)
	}
	if group != "moe" {
		t.Errorf("wrong user, got '%s', expected 'moe'\n", group)
	}
}

func TestCreate(t *testing.T) {

	conn := runserver(rootdir, port)

	// User "moe" does not have write permission in parent directory.
	err := create(conn, "moe", "/books", os.ModeDir+0755)
	if err == nil {
		t.Error("moe could create /books.")
	}

	err = create(conn, "adm", "/books", os.ModeDir+0755)
	if err != nil {
		t.Errorf("adm could not create /books")
	}
	err = chown(conn, "moe", "/books")
	if err != nil {
		t.Errorf("adm could not chown to moe")
	}
	err = create(conn, "moe", "/books/chapter1", os.ModeDir+0755)
	if err != nil {
		t.Errorf("moe could not create /books/chapter1")
	}

	/*
		// User should be user, group should come from directory.
		user, group, err := usergroup(conn, "/books", "adm")
		if err != nil {
			t.Errorf("%s: usergroup('%s'): %s\n", tt, tt.path, err)
		}
		if user != tt.user {
			t.Errorf("%s: wrong user, got '%s', expected '%s'\n",
				tt, user, tt.user)
		}
		if group != tt.group {
			t.Errorf("%s: wrong group, got '%s', expected '%s'\n",
				tt, user, tt.user)
		}
	*/

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

		case "delete":
			err := delete(conn, tt.user, tt.path)
			if tt.allowed {
				if err != nil {
					t.Errorf("%s: %v\n", tt, err)
				}
				fp, err := os.Open(rootdir + "/" + tt.path)
				if err == nil {
					fp.Close()
					t.Errorf("%s: delete failed\n", tt)
				} else if !os.IsNotExist(err) {
					t.Errorf("%s: after delete, err != IsNotExist: %v\n", tt, err)
				}

			} else {
				if err == nil {
					t.Errorf("%s: was allowed\n", tt)
				}
			}

			// User should be user, group should come from directory.

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
					t.Errorf("%s: exp = '%s', act = '%s'\n", tt, f.contents, contents)
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

	/*



		{false, "larry", "adm", "create", os.ModeDir + 0700, "/books/larry", true},
		{false, "larry", "adm", "create", 0600, "/books/larry/draft", true},
		{false, "moe", "adm", "create", 0600, "/books/larry/moe-draft", true},


		// Delete files
		{false, "moe", "adm", "create", os.ModeDir + 0755, "/books", false},
		{false, "larry", "adm", "create", os.ModeDir + 0700, "/books/larry", true},
		{false, "larry", "adm", "create", 0600, "/books/larry/draft", true},
		{false, "moe", "", "delete", 0600, "/books/larry/draft", true},
		{false, "adm", "", "delete", 0600, "/books/larry/draft", true},
		{true, "larry", "", "delete", 0600, "/books/larry/draft", true},
	*/
}
