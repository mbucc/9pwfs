/*
   Copyright (c) 2015, Mark Bucciarelli <mkbucc@gmail.com>
*/

package vufs

import (
	. "github.com/smartystreets/goconvey/convey"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
)

func TestNoUidGid(t *testing.T) {

	Convey("Given a user pool and a file", t, func() {

		path := "t.txt"

		root, err := ioutil.TempDir("", "vufs")
		So(err, ShouldBeNil)

		fn := filepath.Join(root, usersfn)
		os.MkdirAll(filepath.Dir(fn), 0700)
		err = ioutil.WriteFile(fn, []byte("1:adm:\n2:mark:\n3:nuts:\n"), 0644)
		So(err, ShouldBeNil)

		users, err := NewVusers(root)
		So(err, ShouldBeNil)

		Convey("If no uidgid file found, then file owned by adm", func() {

			os.Remove(uidgidFile)

			user, group, err := path2UidGid(path, users)
			So(err, ShouldBeNil)

			So(user, ShouldEqual, "adm")
			So(group, ShouldEqual, "adm")
		})

		Convey("If uidgid file has entry for the file, we use that user and group", func() {

			ioutil.WriteFile(uidgidFile, []byte(path+":2:3"), 0644)

			user, group, err := path2UidGid(path, users)
			So(err, ShouldBeNil)

			So(user, ShouldEqual, "mark")
			So(group, ShouldEqual, "nuts")
		})

		Reset(func() {
			defer os.RemoveAll(root)
		})
	})

}
