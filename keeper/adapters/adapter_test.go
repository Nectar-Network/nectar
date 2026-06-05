package adapters

import "testing"

func TestSortByPriority_Descending(t *testing.T) {
	tasks := []Task{
		{Target: "a", Priority: 1},
		{Target: "b", Priority: 10},
		{Target: "c", Priority: 4},
	}
	SortByPriority(tasks)
	got := []string{tasks[0].Target, tasks[1].Target, tasks[2].Target}
	want := []string{"b", "c", "a"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order mismatch at %d: got %v want %v", i, got, want)
		}
	}
}

func TestSortByPriority_Stable(t *testing.T) {
	tasks := []Task{
		{Target: "a", Priority: 5},
		{Target: "b", Priority: 5},
		{Target: "c", Priority: 5},
	}
	SortByPriority(tasks)
	for i, want := range []string{"a", "b", "c"} {
		if tasks[i].Target != want {
			t.Fatalf("stability broken at %d: %v", i, tasks[i].Target)
		}
	}
}
