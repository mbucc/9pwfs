/*
   Copyright (c) 2015, Mark Bucciarelli <mkbucc@gmail.com>
*/

package vufs


import (
	. "github.com/smartystreets/goconvey/convey"
	"io/ioutil"
	"testing"
	"os"
)

func TestNoUidGid(t *testing.T) {

	Convey("Given a file name", t, func() {

		path := "t.txt"

		Convey("No uidgid file means it is owned by adm", func() {

			os.Remove(uidgidFile)

			uid, gid, _ := path2UidGid(path)

			So(uid, ShouldEqual, "adm")
			So(gid, ShouldEqual, "adm")
		})

		Convey("We use the uidgid entry if first column matches filename", func() {

			ioutil.WriteFile(uidgidFile, []byte(path + ":mark:nuts"), 0644)

			uid, gid, _ := path2UidGid(path)

			So(uid, ShouldEqual, "mark")
			So(gid, ShouldEqual, "nuts")
		})

		Reset(func() {
			os.Remove(uidgidFile)
		})

	})


}