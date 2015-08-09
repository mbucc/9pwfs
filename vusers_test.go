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


		})
	})

}
