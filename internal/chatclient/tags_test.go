package chatclient

import "testing"

func TestTagMapAssignResolveReset(t *testing.T) {
	tm := NewTagMap()

	tag0 := tm.Assign(100)
	if tag0 != "a0" {
		t.Fatalf("first tag = %q, want %q", tag0, "a0")
	}
	tag1 := tm.Assign(101)
	if tag1 != "a1" {
		t.Fatalf("second tag = %q, want %q", tag1, "a1")
	}

	// Re-assigning the same message id returns the same tag rather than a new one.
	if again := tm.Assign(100); again != tag0 {
		t.Fatalf("re-assign(100) = %q, want %q", again, tag0)
	}

	id, ok := tm.Resolve("a1")
	if !ok || id != 101 {
		t.Fatalf("Resolve(a1) = (%d, %v), want (101, true)", id, ok)
	}
	if _, ok := tm.Resolve("zz"); ok {
		t.Fatal("Resolve(zz) should not be found before assignment")
	}

	tm.Reset()
	if _, ok := tm.Resolve("a0"); ok {
		t.Fatal("Resolve(a0) should not be found after Reset")
	}
	reassigned := tm.Assign(999)
	if reassigned != "a0" {
		t.Fatalf("first tag after Reset = %q, want %q", reassigned, "a0")
	}
}

func TestTagMapRolloverToThreeDigits(t *testing.T) {
	tm := NewTagMap()
	var last string
	// Tags start at "a0" (offset 360 = 10*36). 26*36 = 936 assignments exhausts every
	// two-digit tag from "a0" through "z9"/"zz"; the next assignment must grow to three
	// base36 digits rather than colliding with an already-rendered tag.
	const twoDigitCount = 26 * 36
	for i := int64(0); i < twoDigitCount; i++ {
		last = tm.Assign(i)
	}
	if len(last) != 2 {
		t.Fatalf("last two-digit tag = %q, want length 2", last)
	}
	overflow := tm.Assign(twoDigitCount)
	if len(overflow) != 3 {
		t.Fatalf("overflow tag = %q, want length 3 (rolled into next base36 digit width)", overflow)
	}
	id, ok := tm.Resolve(overflow)
	if !ok || id != twoDigitCount {
		t.Fatalf("Resolve(%q) = (%d, %v), want (%d, true)", overflow, id, ok, twoDigitCount)
	}
}
