/*
   Copyright (c) 2015, Mark Bucciarelli <mkbucc@gmail.com>
*/

package vufs

import (
	. "github.com/smartystreets/goconvey/convey"
	"testing"
)

func TestNoUidGid(t *testing.T) {

	Convey("Given a file name with no uid, gid.", t, func() {

		path := "t.txt"

		Convey("It should be owned by adm user", func() {

			name, uid, gid := path2UidGuidName(path)

			So(name, ShouldEqual, "t.txt")
			So(uid, ShouldEqual, "adm")
			So(gid, ShouldEqual, "adm")
		})
	})

}


func TestJustUid(t *testing.T) {

	Convey("Given a file name with just a  uid", t, func() {

		path := "t.txt" + delim + "mark"

		Convey("The group should be same as uid", func() {

			name, uid, gid := path2UidGuidName(path)

			So(name, ShouldEqual, "t.txt")
			So(uid, ShouldEqual, "mark")
			So(gid, ShouldEqual, "mark")
		})
	})
}

func TestBothUidGid(t *testing.T) {

	Convey("Given a file name with both a uid and gid", t, func() {

		path := "t.txt" + delim + "mark" + delim + "nuts"

		Convey("The uid and gid should be parsed correctly", func() {

			name, uid, gid := path2UidGuidName(path)

			So(name, ShouldEqual, "t.txt")
			So(uid, ShouldEqual, "mark")
			So(gid, ShouldEqual, "nuts")
		})
	})
}
