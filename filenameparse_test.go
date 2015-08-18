/*
   Copyright (c) 2015, Mark Bucciarelli <mkbucc@gmail.com>
*/

package vufs

import (
	. "github.com/smartystreets/goconvey/convey"
	"io/ioutil"
	"testing"
)

func TestNoUidGid(t *testing.T) {

	Convey("Given a file name with no uid, gid.", t, func() {

		path := "t.txt"

		Convey("It should be owned by adm user", func() {

			uid, gid, _ := path2UidGid(path)

			So(uid, ShouldEqual, "adm")
			So(gid, ShouldEqual, "adm")
		})
	})

}


func TestUidGid(t *testing.T) {

	Convey("Given a file name with just a  uid", t, func() {

		path := "t.txt"
		ioutil.WriteFile(uidgidFile, []byte("t.txt:mark:mark"), 0644)

		Convey("The group should be same as uid", func() {

			uid, gid, _ := path2UidGid(path)

			So(uid, ShouldEqual, "mark")
			So(gid, ShouldEqual, "mark")
		})
	})
}