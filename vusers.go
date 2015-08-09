// Copyright 2009 The Go9p Authors.
// Copyright 2015 Mark Bucciarelli <mkbucc@gmail.com>
// All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vufs

import (
	"bytes"
	"fmt"
	"github.com/rminnich/go9p"
	"io/ioutil"
	"path/filepath"
	"strconv"
	"sync"
)

const (
	usersfn = "adm/users"
)

var badchar = []rune{'?', '=', '+', '–', '/', ':'}

// A user is a group with one member.
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
	root       string
	nameToUser map[string]*vUser
	idToUser   map[int]*vUser
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
	up.Lock()
	defer up.Unlock()
	user, present := up.idToUser[uid]
	if present {
		return user
	}
	return nil
}

func (up *vUsers) Uname2User(uname string) go9p.User {
	up.Lock()
	defer up.Unlock()
	user, present := up.nameToUser[uname]
	if present {
		return user
	}
	return nil
}

func (up *vUsers) Gid2Group(gid int) go9p.Group {
	up.Lock()
	defer up.Unlock()
	group, present := up.idToUser[gid]
	if present {
		return group
	}
	return nil
}

func (up *vUsers) Gname2Group(gname string) go9p.Group {
	up.Lock()
	defer up.Unlock()
	group, present := up.nameToUser[gname]
	if present {
		return group
	}
	return nil
}

func NewVusers(root string) (*vUsers, error) {

	fullpath := filepath.Join(root, usersfn)
	data, err := ioutil.ReadFile(fullpath)
	if err != nil {
		return nil, err
	}

	nameToUser := make(map[string]*vUser)

	lines := bytes.Split(data, []byte("\n"))
	for idx, line := range lines {

		if len(line) == 0 {
			continue
		}

		if line[0] == byte('#') {
			continue
		}

		columns := bytes.Split(line, []byte(":"))
		if len(columns) != 3 {
			return nil, fmt.Errorf("Got %d columns (expected %d) on line %d of %s",
				len(columns), 3, idx, fullpath, string(line))
		}

		id, err := strconv.Atoi(string(columns[0]))
		if err != nil {
			return nil, fmt.Errorf("Can't parse first column as integer on line %d of %s",
				len(columns), 3, idx, fullpath, string(line))
		}
		name := string(columns[1])
		nameToUser[name] = &vUser{
			id:      id,
			name:    name,
			members: make([]go9p.User, 0),
			groups:  make([]go9p.Group, 0)}
	}

	// Load groups on second pass.
	lines = bytes.Split(data, []byte("\n"))
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		if line[0] == byte('#') {
			continue
		}
		columns := bytes.Split(line, []byte(":"))
		name := string(columns[1])
		groups := columns[2]
		user, present := nameToUser[name]
		if !present {
			panic(fmt.Sprintf("can't find user '%s' after first pass", name))
		}
		groupNames := bytes.Split(groups, []byte(","))
		for _, groupName := range groupNames {
			if len(groupName) == 0 {
				continue
			}
			group, present := nameToUser[string(groupName)]
			if !present {
				panic(fmt.Sprintf("can't find group name '%s' after first pass", groupName))
			}
			user.groups = append(user.groups, group)
		}
	}

	idToUser := make(map[int]*vUser, len(nameToUser))
	for _, user := range nameToUser {
		idToUser[user.Id()] = user
	}

	return &vUsers{
		root:       root,
		nameToUser: nameToUser,
		idToUser:   idToUser}, nil
}
