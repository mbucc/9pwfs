/*
   Copyright (c) 2015, Mark Bucciarelli <mkbucc@gmail.com>
*/

package vufs

import (
	. "github.com/smartystreets/goconvey/convey"
	"testing"
)

func TestUserFileLoaded(t *testing.T) {

	Convey("Given a valid users file", t, func() {

		users, _ := NewVusers("./test")

		Convey("It should be parsed properly", func() {

			So(users.Uname2User("adm"), ShouldNotBeNil)

			So(users.Uid2User(5), ShouldNotBeNil)
			So(users.Uid2User(5).Name(), ShouldEqual, "glenda")

			user := users.Uname2User("mark")
			So(user, ShouldNotBeNil)
			So(len(user.Groups()), ShouldEqual, 2)
			So(user.Groups()[0].Name(), ShouldEqual, "adm")
			So(user.Groups()[1].Name(), ShouldEqual, "sys")

			group := users.Gname2Group("sys")
			So(group, ShouldNotBeNil)

			// A user is always in it's own group.
			So(len(group.Members()), ShouldEqual, 2)
			So(group.Members()[0].Name(), ShouldEqual, "adm")
			So(group.Members()[1].Name(), ShouldEqual, "mark")


		})
	})

}
