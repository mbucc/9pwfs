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
	//fs.Debuglevel = 5

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

	// file modes: ../../lionkov/go9p/p/p9.go:65,74
	fid, err := fsys.Open("/", plan9.OREAD)
	if err != nil {
		return nil, err
	}
	defer fid.Close()

	// returns an array of Dir instances: ../../lionkov/go9p/p/clnt/read.go:88
	d, err := fid.Dirreadall()
	if err != nil {
		return nil, err
	}

	return d, nil

}

func TestServer(t *testing.T) {

	rootdir := "./tmpfs"

	initfs(rootdir, 0755, "1:adm:adm\n2:mark:mark\n")

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

	})

	conn.Close()

	os.RemoveAll(rootdir)

}
