// Copyright 2009 The Go9p Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vufs

import (
	"github.com/rminnich/go9p"
	"sync"
)

const (
	userfn = "adm/users" 
)

var 	badchar = []rune{'?', '=', '+', '–', '/', ':' }


// In plan9, users and groups are the same.  A user is a group with one member.
// ref: https://swtch.com/plan9port/man/man8/fossilcons.html
type vUser struct {
	// An integer used to represent this user in the on-disk structures
	// This should never change.
	id int
	// The string used to represent this user in the 9P protocol.
	// This can change, for example if a user changes their name.
	// (Renaming is not currently supported.)
	name string
	// A comma-separated list of members in this group
	members []go9p.User
	// A comma-separated list of groups this user is part of.
	groups []go9p.Group
}

// Simple go9p.Users implementation of virtual users.
type vUsers struct {
	root  string
	users map[string]*vUser
	sync.Mutex
}

/*
../../rminnich/go9p/p9.go:192,198
// Represents a user
type User interface {
	Name() string          // user name
	Id() int               // user id
	Groups() []Group       // groups the user belongs to (can return nil)
	IsMember(g Group) bool // returns true if the user is member of the specified group
}

../../rminnich/go9p/p9.go:200,205
// Represents a group of users
type Group interface {
	Name() string    // group name
	Id() int         // group id
	Members() []User // list of members that belong to the group (can return nil)
}
*/

func (u *vUser) Name() string { return u.name }

func (u *vUser) Id() int { return u.id }

func (u *vUser) Groups() []go9p.Group { return u.groups }

func (u *vUser) Members() []go9p.User { return u.members }

func (u *vUser) IsMember(g go9p.Group) bool {
	for _, b := range u.groups {
		if b.Id() == g.Id() {
			return true
		}
	}
	return false
}

/*
../../rminnich/go9p/p9.go:184,190
// Interface for accessing users and groups
type Users interface {
	Uid2User(uid int) User
	Uname2User(uname string) User
	Gid2Group(gid int) Group
	Gname2Group(gname string) Group
}
*/

func (up *vUsers) Uid2User(uid int) go9p.User {
	panic("Uid2User should not be called, not using dotu")
}

func (up *vUsers) Uname2User(uname string) go9p.User {
	up.Lock()
	defer up.Unlock()
	user, present := up.users[uname]
	if present {
		return user
	}
	return nil
}

func (up *vUsers) Gid2Group(gid int) go9p.Group {
	panic("Gid2Group should not be called, not using dotu")
}

func (up *vUsers) Gname2Group(gname string) go9p.Group {
	up.Lock()
	defer up.Unlock()
	group, present := up.users[gname]
	if present {
		return group
	}
	return nil
}

func NewVusers(root string) *vUsers {
	return &vUsers{root: root, users: make(map[string]*vUser)}
}
