/*
   Copyright (c) 2015, Mark Bucciarelli <mkbucc@gmail.com>
*/

package vufs_test

import (
	"testing"
	"github.com/mbucc/vufs"
)

func TestUserFileLoaded(t *testing.T) {

	users, _ := vufs.NewVusers("./test")

	if users.Uname2User("adm") == nil {
		t.Error("Uname2User(\"adm\") was nil")
	}

	if users.Uid2User(5) == nil {
		t.Error("users.Uid2User(5) was nil")
	} else {
		if users.Uid2User(5).Name() != "glenda" {
			t.Error("users.Uid2User(5).Name() != glenda")
		}
	}

	if u := users.Uname2User("mark"); u == nil {
		t.Error("users.Uname2User(\"mark\") was nil")
	} else {
		if u.Name() != "mark" {
			t.Error("users.Uid2User(5).Name() != mark")
		}
		if len(u.Groups()) != 2 {
			t.Error("user mark didn't have two groups")
		}
		if u.Groups()[0].Name() != "adm" {
			t.Error("mark: first group wasn't adm")
		}
		if u.Groups()[1].Name() != "sys" {
			t.Error("mark: second group wasn't sys")
		}
	}

	if g := users.Gname2Group("sys"); g == nil {
		t.Error("users.Gname2Group(\"sys\") was nil")

	} else {
		if len(g.Members()) != 2 {
			t.Error("group sys: didn't have two members")
		}
		if g.Members()[0].Name() != "adm" {
			t.Error("group sys: first member wasn't adm")
		}
		if g.Members()[1].Name() != "mark" {
			t.Error("group sys: second member wasn't mark")
		}
	}
}
