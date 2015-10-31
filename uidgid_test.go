/*
   Copyright (c) 2015, Mark Bucciarelli <mkbucc@gmail.com>
*/

package vufs_test


import (
	"io/ioutil"
	//"os"
	//"path/filepath"
	//"testing"
)

var rootdir string

func init() {
	var err error
	rootdir, err = ioutil.TempDir("", "vufs")
	if err != nil {
		panic(err)
	}
}

/*
func TestAdmIsDefaultOwner(t *testing.T) {

	err := os.RemoveAll(rootdir)
	if err != nil {
		t.Errorf("RemoveAll(%s): %v\n", rootdir, err)
	}

	err = os.MkdirAll(filepath.Dir(rootdir), 0700)
	if err != nil {
		t.Errorf("MkdirAll(%s): %v\n", rootdir, err)

	}
	defer os.RemoveAll(rootdir)

	users, err := NewVusers(rootdir)
	if err != nil {
		t.Errorf("NewVusers(%s): %v\n", rootdir, err)

	}

	user, group, err := path2UserGroup(rootdir + "/t.txt", users)
	if err != nil {
		t.Errorf("path2UserGroup(t.txt): err = %v\n", err)

	}

	if user != "adm" {
		t.Error("user != adm")
	}

	if group != "adm" {
		t.Error("group != adm")
	}

}

func TestUidGidHasEntry(t *testing.T) {

	err := os.RemoveAll(rootdir)
	if err != nil {
		t.Errorf("RemoveAll(%s): %v\n", rootdir, err)
	}

	d := rootdir + "/" + filepath.Dir(usersFile)
	err = os.MkdirAll(d, 0755)
	if err != nil {
		t.Fatalf("MkdirAll(%s): %v\n", d, err)
	}
	defer os.RemoveAll(rootdir)


	fn := rootdir + "/" + usersFile
	err = ioutil.WriteFile(fn, []byte("1:adm:\n2:mark:\n3:nuts:\n"), 0644)
	if err != nil {
		t.Fatalf("WriteFile(%s): err = %v\n", fn, err)
	}

	err = ioutil.WriteFile(rootdir + "/" + uidgidFile, []byte("t.txt:2:3"), 0644)
	if err != nil {
		t.Fatalf("WriteFile(%s): err = %v\n", rootdir + "/" + uidgidFile, err)
	}

	users, err := NewVusers(rootdir)
	if err != nil {
		t.Errorf("NewVusers(%s): %v\n", rootdir, err)

	}

	user, group, err := path2UserGroup(rootdir + "/t.txt", users)
	if err != nil {
		t.Errorf("path2UserGroup(%s): err = %v\n", rootdir + "/t.txt", err)

	}

	if user != "mark" {
		t.Errorf("user: '%s' != 'mark'\n", user)
	}

	if group != "nuts" {
		t.Errorf("group: '%s' != 'nuts'\n", group)
	}
}

*/
