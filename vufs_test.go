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

func initfs(rootdir, userdata string) {
	os.Mkdir(rootdir, 0755)
	os.Mkdir(rootdir+"/adm", 0700)
	ioutil.WriteFile(rootdir+"/adm/users", []byte(userdata), 0600)
	ioutil.WriteFile(rootdir+"/adm/"+uidgidFile, []byte("1:users:adm:adm\n"), 0600)
}

func server(rootdir, port string) {
	var err error
	fs := new(VuFs)
	fs.Id = "vufs"
	fs.Root = rootdir
	//fs.Debuglevel = 1
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

func TestServerGid(t *testing.T) {

	var err error

	Convey("Given a vufs rooted in a 755 directory and a client", t, func() {

		rootdir := "./tmpfs"

		initfs(rootdir, "1:adm:adm\n2:mark:mark\n")

		server(rootdir, port)
		So(err, ShouldBeNil)

		Convey("A valid user can list files", func() {

			validUser := &vUser{
				id:      2,
				name:    "mark",
				members: []go9p.User{},
				groups:  []go9p.Group{}}

			dirs, err := listDir(".", validUser)
			So(err, ShouldBeNil)
			So(len(dirs), ShouldEqual, 1)
			So(dirs[0].Name, ShouldEqual, "adm")
		})

		Reset(func() {
			os.RemoveAll(rootdir)
		})

	})
}
