// Copyright 2009 The Go9p Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vufs

import (
	"sync"
	"github.com/rminnich/go9p"
)


var once sync.Once

type vuUser struct {
	uid int
}

type vuUsers struct {
	users  map[int]*vuUser
	groups map[int]*osGroup
	sync.Mutex
}

// Simple go9p.Users implementation that fakes looking up users and groups
// by uid only. The names and groups memberships are empty
var VuUsers *vuUsers

func (u *vuUser) Name() string { return "" }

func (u *vuUser) Id() int { return u.uid }

func (u *vuUser) Groups() []go9p.Group { return nil }

func (u *vuUser) IsMember(g go9p.Group) bool { return false }

type osGroup struct {
	gid int
}

func (g *osGroup) Name() string { return "" }

func (g *osGroup) Id() int { return g.gid }

func (g *osGroup) Members() []go9p.User { return nil }

func initOsusers() {
	VuUsers = new(vuUsers)
	VuUsers.users = make(map[int]*vuUser)
	VuUsers.groups = make(map[int]*osGroup)
}

func (up *vuUsers) Uid2User(uid int) go9p.User {
	once.Do(initOsusers)
	VuUsers.Lock()
	defer VuUsers.Unlock()
	user, present := VuUsers.users[uid]
	if present {
		return user
	}

	user = new(vuUser)
	user.uid = uid
	VuUsers.users[uid] = user
	return user
}

func (up *vuUsers) Uname2User(uname string) go9p.User {
	// unimplemented
	return nil
}

func (up *vuUsers) Gid2Group(gid int) go9p.Group {
	once.Do(initOsusers)
	VuUsers.Lock()
	group, present := VuUsers.groups[gid]
	if present {
		VuUsers.Unlock()
		return group
	}

	group = new(osGroup)
	group.gid = gid
	VuUsers.groups[gid] = group
	VuUsers.Unlock()
	return group
}

func (up *vuUsers) Gname2Group(gname string) go9p.Group {
	return nil
}
