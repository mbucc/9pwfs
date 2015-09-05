/*
   Copyright (c) 2015, Mark Bucciarelli <mkbucc@gmail.com>
*/

package vufs

import (
	"fmt"
	. "github.com/smartystreets/goconvey/convey"
	"io/ioutil"
	"io"
	"net"
	"os"
	"testing"

	"github.com/lionkov/go9p/p"
	"github.com/lionkov/go9p/p/clnt"
)

const (
	port               = ":5000"
	messageSizeInBytes = 8192
)

func initfs(rootdir string, mode os.FileMode, userdata string) {
	os.Mkdir(rootdir, mode)
	os.Mkdir(rootdir+"/adm", 0700)
	ioutil.WriteFile(rootdir+"/adm/users", []byte(userdata), 0600)
	ioutil.WriteFile(rootdir+"/adm/"+uidgidFile, []byte("1:users:adm:adm\n"), 0600)
}

func runserver(rootdir, port string) {

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
	var conn net.Conn
	for i := 0; i < 16; i++ {
		if conn, err = net.Dial("tcp", port); err == nil {
			fmt.Printf("Server is up, got connnection %+v\n", conn)
			conn.Close()
			break
		}
	}
	if err != nil {
		panic("couldn't connect to runserver after 15 tries")
	}
}

func listDir(path string, user p.User) ([]*p.Dir, error) {

	client, err := clnt.Mount("tcp", port,  "/", messageSizeInBytes, user)
	if err != nil {
		return nil, err
	}
	defer client.Unmount()

	// file modes: ../../lionkov/go9p/p/p9.go:65,74
	file, err := client.FOpen("/", p.OREAD)
	if err != nil && err != io.EOF  {
		return nil, err
	}

	// returns an array of Dir instances: ../../lionkov/go9p/p/clnt/read.go:88
	d, err := file.Readdir(0)
	if err != nil && err != io.EOF {
		return nil, err
	}

	return d, nil

}

func TestServer(t *testing.T) {

	rootdir := "./tmpfs"

	initfs(rootdir, 0755, "1:adm:adm\n2:mark:mark\n")

	runserver(rootdir, port)

	mark := &vUser{
		id:      2,
		name:    "mark",
		members: []p.User{},
		groups:  []p.Group{}}

	hugo := &vUser{
		id:      3,
		name:    "hugo",
		members: []p.User{},
		groups:  []p.Group{}}


	Convey("Given a vufs rooted in a directory and a client", t, func() {
		var d []*p.Dir
	
		Convey("A valid user can list the one file in a 0755 root directory", func() {
			os.Chmod(rootdir, 0755)
			dirs, err := listDir("/", mark)
			So(err, ShouldBeNil)
			So(len(dirs), ShouldEqual, 1)
		})

		Convey("An invalid user cannot list root directory", func() {
			os.Chmod(rootdir, 0777)
			_, err := listDir(".", hugo)
			So(err, ShouldNotBeNil)
		})

		Convey("A valid user without permissions cannot list files", func() {
			err := os.Chmod(rootdir, 0700)
			So(err, ShouldBeNil)
			d, err = listDir(".", mark)
			So(err, ShouldNotBeNil)
		})
	})

	os.RemoveAll(rootdir)

}
