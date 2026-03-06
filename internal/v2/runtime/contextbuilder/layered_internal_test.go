package contextbuilder

import "testing"

func TestClampRefList_DedupAndLimit(t *testing.T) {
	t.Parallel()

	in := []string{
		"turn/t1",
		"turn/t2",
		"turn/t1",
		"turn/t3",
		"turn/t4",
	}
	got := clampRefList(in, 3)

	if len(got) != 3 {
		t.Fatalf("len(got)=%d want 3", len(got))
	}
	want := []string{"turn/t1", "turn/t2", "turn/t3"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d]=%q want %q (full=%v)", i, got[i], want[i], got)
		}
	}
}
