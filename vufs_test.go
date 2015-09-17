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

// Initialize file system as:
//
//          /
//           |
//           +-- adm/            --rwx------ adm adm
//           |       |
//           |       +-- users     --rw------- adm adm
//           |
//           +-- .uidgid          --rw-rw---- adm mark
//           |
//           +-- whatever.txt     --rw-rw-r-- adm mark
//
//         Notes:
//
//          a.    Users shown are virtual ones, not ones on disk.
//
//          b.    If no ownership specified (in .uidgid), it defaults to adm adm.
//
//
func initfs(rootdir string, userdata string) {

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

	err = os.Mkdir(rootdir+"/adm", 0700)
	if err != nil {
		msg := fmt.Sprintf("os.Mkdir(%s, 0700) failed: %v\n", rootdir+"/adm", err)
		panic(msg)
	}

	err = ioutil.WriteFile(rootdir+"/adm/users", []byte(userdata), 0600)
	if err != nil {
		msg := fmt.Sprintf("ioutil.WriteFile(%s, 0600) failed: %v\n", 
				rootdir+"/adm/users", err)
		panic(msg)
	}

	err = ioutil.WriteFile(rootdir+"/whatever.txt", []byte("whatever"), 0664)
	if err != nil {
		msg := fmt.Sprintf("ioutil.WriteFile(%s, 0664) failed: %v\n", 
				rootdir+"/whatever.txt", err)
		panic(msg)
	}

	err = ioutil.WriteFile(rootdir+"/"+uidgidFile, []byte("whatever.txt:2:2\n"), 0600)
	if err != nil {
		msg := fmt.Sprintf("ioutil.WriteFile(%s, 0600) failed: %v\n", 
			rootdir+"/whatever.txt", err)
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

/*

func TestServer(t *testing.T) {

	initfs(rootdir, 0755, "1:adm:adm\n2:mark:mark\n3:other:other\n")

	conn := runserver(rootdir, port)

	Convey("Given a vufs rooted in a directory and a client", t, func() {

		var dirs []*plan9.Dir
		var err error

		Convey("Given an 0755 root and the file: 0600 mark mark whatever.txt", func() {

			os.Chmod(rootdir, 0755)

			fn := "whatever.txt"

			realpath := rootdir+"/" + fn

			os.RemoveAll(realpath)

			ioutil.WriteFile(realpath, []byte("whatever"), 0600)

			ioutil.WriteFile(rootdir+"/"+uidgidFile, []byte(fn + ":2:2\n"), 0600)

			Convey("adm should not be able to read it ", func() {

				_, err := read(conn, "adm", "/" + fn)

				So(err.Error(), ShouldEqual, "permission denied: 1")

			})

			Convey("mark should be able to read it ", func() {

				contents, err := read(conn, "mark", fn)

				So(err, ShouldBeNil)

				So(contents, ShouldEqual, "whatever")

			})


			Convey("other should not be able to read it ", func() {

				_, err := read(conn, "other", fn)

				So(err.Error(), ShouldEqual, "permission denied: 1")

			})


			Convey("mark should be able to write to it ", func() {

				n, newcontents, err := write(conn, "mark", fn, "whom")

				So(err, ShouldBeNil)

				So(n, ShouldEqual, 4)

				So(newcontents, ShouldEqual, "whomever")

			})


			Convey("adm and other should not be able to write it ", func() {

				users := []string{"adm","other"}

				for i := range users {

					_, _, err = write(conn, users[i], fn, "whom")

					So(err.Error(), ShouldEqual, "permission denied: 1")
				}

			})

		})



		Convey("Given an 0755 root and the file: 0664 adm mark whatever.txt", func() {

			os.Chmod(rootdir, 0755)

			fn := "whatever.txt"

			os.RemoveAll(rootdir+"/" + fn)
			ioutil.WriteFile(rootdir+"/" + fn, []byte("whatever"), 0664)
			ioutil.WriteFile(rootdir+"/"+uidgidFile, []byte(fn + ":1:2\n"), 0600)

			Convey("adm, mark and other should be able to read it ", func() {

				users := []string{"mark", "adm","other"}

				for i := range users {

					contents, err := read(conn, users[i], fn)

					So(err, ShouldBeNil)

					So(contents, ShouldEqual, "whatever")

				}

			})

			Convey("adm and mark should be able to write it ", func() {

				users := []string{"mark", } //"adm" }

				for i := range users {

					n, newcontents, err := write(conn, users[i], fn, "whom")

					So(err, ShouldBeNil)

					So(n, ShouldEqual, 4)

					So(newcontents, ShouldEqual, "whomever")

			}

			})
		})


// adm mark other
// 0400	read: Y N N	write: N N N
// 0600	read: Y N N	write: Y N N
// 0640	read: Y Y N	write: Y N N
// 0644	read: Y Y Y	write: Y N N
// 0664	read: Y Y N	write: Y Y N
// 0666	read: Y Y N	write: Y Y N

	})

	conn.Close()

	//os.RemoveAll(rootdir)

}

*/

func TestFiles(t *testing.T) {

	initfs(rootdir, "1:adm:adm\n2:mark:mark\n3:other:other\n")

	conn := runserver(rootdir, port)

	for _, tt := range fileTests {

		err := os.Chmod(rootdir+tt.path, tt.mode)

		if err != nil {
			t.Errorf("%+v: chmod failed: %v\n", tt, err)
		}

		switch tt.op {

		default:
			t.Errorf("Unsupported operation in %+v\n", tt)

		case "read":
			contents, err := read(conn, tt.user, tt.path)
			if tt.allowed {
				if err != nil {
					t.Errorf("%v expected to be able to read, got %v\n", tt, err)
				} else if contents != expectedContents[tt.path] {
					t.Errorf("%v expected '%s', got '%s'\n", 
							tt, expectedContents[tt.path], contents)
				} else {
					// EMPTY --- test passed.
				}
			} else {
				if err == nil {
					t.Errorf("%+v: should have gotten an error\n", tt)
				} else {
					// EMPTY --- test passed.
				}
			}
		}
	}
}

var expectedContents = map[string]string {
	"/": ".uidgid, adm, whatever.txt",
	"/whatever.txt": "whatever",
}

var fileTests = []struct {
	path    string
	mode    os.FileMode
	op      string
	user    string
	allowed bool
}{
	// Root directory
	{"/", 0700, "read", "adm", true},
	{"/", 0700, "read", "mark", false},
	{"/", 0700, "read", "other", false},

	{"/", 0750, "read", "adm", true},
	{"/", 0750, "read", "mark", false},
	{"/", 0750, "read", "other", false},

	{"/", 0755, "read", "adm", true},
	{"/", 0755, "read", "mark", true},
	{"/", 0755, "read", "other", true},

	// text file owner = mark, group = mark
	{"/whatever.txt", 0600, "read", "adm", false},
	{"/whatever.txt", 0600, "read", "mark", true},
	{"/whatever.txt", 0600, "read", "other", false},

	{"/whatever.txt", 0400, "read", "adm", false},
	{"/whatever.txt", 0400, "read", "mark", true},
	{"/whatever.txt", 0400, "read", "other", false},

	{"/whatever.txt", 0440, "read", "adm", false},
	{"/whatever.txt", 0440, "read", "mark", true},
	{"/whatever.txt", 0440, "read", "other", false},

	{"/whatever.txt", 0444, "read", "adm", true},
	{"/whatever.txt", 0444, "read", "mark", true},
	{"/whatever.txt", 0444, "read", "other", true},

}
