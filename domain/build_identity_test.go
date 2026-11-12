package domain

import "testing"

func TestBuildIdentityKnownAndMatches(t *testing.T) {
	a := BuildIdentity{Revision: "abc", Time: "t1"}
	if !a.Known() || (BuildIdentity{}).Known() {
		t.Fatal("Known is having a revision")
	}
	if !a.Matches(BuildIdentity{Revision: "abc", Time: "t2", Version: "v"}) {
		t.Fatal("the same commit in the same state matches whatever the time or version")
	}
	if a.Matches(BuildIdentity{Revision: "abd"}) || a.Matches(BuildIdentity{Revision: "abc", Modified: true}) {
		t.Fatal("another commit, or the same one with changes, does not match")
	}
}
