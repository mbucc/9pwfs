/*
   Copyright (c) 2015, Mark Bucciarelli <mkbucc@gmail.com>
*/

package vufs

import (
	"fmt"
	"github.com/mbucc/go9p"
	. "github.com/smartystreets/goconvey/convey"
	"io/ioutil"
	"net"
	"os"
	"testing"
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

func server(rootdir, port string) {

	var err error
	fs := new(VuFs)
	fs.Id = "vufs"
	fs.Root = rootdir
	fs.Upool, err = NewVusers(rootdir)
	if err != nil {
		panic(err)
	}

	fs.Start(fs)
	go func() {
		err = fs.StartNetListener("tcp", port)
		if err != nil {
			panic(err)
		}
	}()

	// Make sure server is listening before returning.
	var conn net.Conn
	for i := 0; i < 16; i++ {
		if conn, err = net.Dial("tcp", port); err == nil {
			fmt.Printf("Server is up, got connnection %+v\n", conn)
			conn.Close()
			break
		}
	}
	if err != nil {
		panic("couldn't connect to server after 15 tries")
	}
}

func listDir(path string, user go9p.User) ([]*go9p.Dir, error) {

	client, err := go9p.Mount("tcp", port,  ".", messageSizeInBytes, user)
	if err != nil {
		return nil, err
	}
	defer client.Unmount()

	// file modes: ../go9p/p9.go:67,76
	file, err := client.FOpen(".", go9p.OREAD)
	if err != nil {
		return nil, err
	}

	// returns an array of Dir instances: ../go9p/p9.go:127,147
	d, err := file.Readdir(0)
	if err != nil {
		return nil, err
	}

	return d, nil

}

func TestServer(t *testing.T) {

	rootdir := "./tmpfs"

	initfs(rootdir, 0755, "1:adm:adm\n2:mark:mark\n")

	server(rootdir, port)

	mark := &vUser{
		id:      2,
		name:    "mark",
		members: []go9p.User{},
		groups:  []go9p.Group{}}

	hugo := &vUser{
		id:      3,
		name:    "hugo",
		members: []go9p.User{},
		groups:  []go9p.Group{}}


	Convey("Given a vufs rooted in a directory and a client", t, func() {
		var d []*go9p.Dir
	
		SkipConvey("A valid user can list a 0755 root directory", func() {
			dirs, err := listDir(".", mark)
			So(err, ShouldBeNil)
			So(len(dirs), ShouldEqual, 1)

		})

		SkipConvey("An invalid user cannot list root directory", func() {
			_, err := listDir(".", hugo)
			So(err, ShouldNotBeNil)
		})

		Convey("With 0700 root dir, a non-adm valid user cannot list files", func() {
			err := os.Chmod(rootdir, 0700)
			So(err, ShouldBeNil)
			d, err = listDir(".", mark)
fmt.Println("d =", d)
			So(err, ShouldNotBeNil)
			os.Chmod(rootdir, 0755)

		})
	})

	os.RemoveAll(rootdir)

}
