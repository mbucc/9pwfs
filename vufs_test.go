/*
   Copyright (c) 2015, Mark Bucciarelli <mkbucc@gmail.com>
*/

package vufs

import (
	"fmt"
	. "github.com/smartystreets/goconvey/convey"
	"io/ioutil"
	"os"
	"testing"

	"9fans.net/go/plan9"
	"9fans.net/go/plan9/client"
)

const (
	port               = ":5000"
	messageSizeInBytes = 8192
)

// Initialize file system as:
//
//          /
//           |
//           +-- adm/            --rwx------ adm adm
//                   |
//                   +-- users     --rw------- adm adm
//
//         Notes:
//
//          a.    Users shown are virtual ones, not ones on disk.
//
//          b.    If no ownership specified (in .uidgid), it defaults to adm adm.
//
//
func initfs(rootdir string, mode os.FileMode, userdata string) {
	os.RemoveAll(rootdir)
	os.Mkdir(rootdir, mode)
	os.Mkdir(rootdir+"/adm", 0700)
	ioutil.WriteFile(rootdir+"/adm/users", []byte(userdata), 0600)
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
			fmt.Printf("Server is up, got connnection %+v\n", conn)
			break
		}

	}

	if err != nil {
		panic("couldn't connect to runserver after 15 tries: " + err.Error())
	}

	return conn
}

func listDir(conn *client.Conn, path, user string) ([]*plan9.Dir, error) {

	fsys, err := conn.Attach(nil, user, "/")
	if err != nil {
		return nil, err
	}

	fid, err := fsys.Open("/", plan9.OREAD)
	if err != nil {
		return nil, err
	}
	defer fid.Close()

	d, err := fid.Dirreadall()
	if err != nil {
		return nil, err
	}

	return d, nil

}

func TestServer(t *testing.T) {

	rootdir := "./tmpfs"

	initfs(rootdir, 0755, "1:adm:adm\n2:mark:mark\n3:other:other\n")

	conn := runserver(rootdir, port)

	Convey("Given a vufs rooted in a directory and a client", t, func() {

		var dirs []*plan9.Dir
		var err error

		Convey("/adm/users is 0600 adm, adm", func() {
			dirs, err = listDir(conn, "/", "adm")
			So(err, ShouldBeNil)
			So(len(dirs), ShouldEqual, 1)
		})

		Convey("A valid user can list the one file in a 0755 root directory", func() {
			os.Chmod(rootdir, 0755)
			dirs, err = listDir(conn, "/", "mark")
			So(err, ShouldBeNil)
			So(len(dirs), ShouldEqual, 1)
		})

		Convey("An invalid user cannot list root directory", func() {
			os.Chmod(rootdir, 0777)
			_, err = listDir(conn, ".", "hugo")
			So(err.Error(), ShouldEqual, "unknown user: 22")
		})

		Convey("A valid user without permissions cannot list files", func() {
			err = os.Chmod(rootdir, 0700)
			So(err, ShouldBeNil)
			dirs, err = listDir(conn, ".", "mark")
			So(err.Error(), ShouldEqual, "permission denied: 1")
		})

		Convey("Given an 0755 root and the file: 0600 mark mark test.txt", func() {
			os.Chmod(rootdir, 0755)

			fn := "test.txt"
			os.RemoveAll(rootdir+"/" + fn)
			ioutil.WriteFile(rootdir+"/" + fn, []byte("whatever"), 0600)
			ioutil.WriteFile(rootdir+"/"+uidgidFile, []byte(fn + ":2:2\n"), 0600)

			Convey("adm should not be able to read it ", func() {

				fsys, err := conn.Attach(nil, "adm", "/")
				So(err, ShouldBeNil)
				_, err = fsys.Open("/" + fn, plan9.OREAD)
				So(err, ShouldNotBeNil)
				So(err.Error(), ShouldEqual, "permission denied: 1")

			})

			Convey("mark should be able to read it ", func() {

				fsys, err := conn.Attach(nil, "mark", "/")
				So(err, ShouldBeNil)
				_, err = fsys.Open("/" + fn, plan9.OREAD)
				So(err, ShouldBeNil)
			})


			Convey("other should not be able to read it ", func() {

				fsys, err := conn.Attach(nil, "other", "/")
				So(err, ShouldBeNil)
				_, err = fsys.Open("/" + fn, plan9.OREAD)
				So(err, ShouldNotBeNil)
				So(err.Error(), ShouldEqual, "permission denied: 1")

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
