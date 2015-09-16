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
	rootdir = "./tmpfs"
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

	contents, err := ioutil.ReadAll(fid)

	return string(contents), err

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

func TestServer(t *testing.T) {

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

		Convey("An invalid user cannot list a 0777 root directory", func() {

			os.Chmod(rootdir, 0777)

			_, err = listDir(conn, ".", "hugo")

			So(err.Error(), ShouldEqual, "unknown user: 22")

		})

		Convey("A valid user can't list a 0700 directory they don't own", func() {

			err = os.Chmod(rootdir, 0700)

			So(err, ShouldBeNil)

			dirs, err = listDir(conn, ".", "mark")

			So(err.Error(), ShouldEqual, "permission denied: 1")

		})

		Convey("Given an 0755 root and the file: 0600 mark mark test.txt", func() {

			os.Chmod(rootdir, 0755)

			fn := "test.txt"

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



		Convey("Given an 0755 root and the file: 0664 adm mark test.txt", func() {

			os.Chmod(rootdir, 0755)

			fn := "test.txt"

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
