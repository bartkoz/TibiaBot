package navigation

import "testing"

func TestPathToDirectionsStraightEast(t *testing.T) {
	path := [][2]int{{100, 100}, {101, 100}, {102, 100}}
	dirs := PathToDirections(path)
	if len(dirs) != 2 {
		t.Fatalf("len = %d, want 2", len(dirs))
	}
	if dirs[0] != East || dirs[1] != East {
		t.Errorf("dirs = %v, want [East, East]", dirs)
	}
}

func TestPathToDirectionsDiagonal(t *testing.T) {
	path := [][2]int{{100, 100}, {101, 101}}
	dirs := PathToDirections(path)
	if len(dirs) != 1 || dirs[0] != Southeast {
		t.Errorf("dirs = %v, want [Southeast]", dirs)
	}
}

func TestPathToDirectionsNorth(t *testing.T) {
	path := [][2]int{{100, 100}, {100, 99}}
	dirs := PathToDirections(path)
	if len(dirs) != 1 || dirs[0] != North {
		t.Errorf("dirs = %v, want [North]", dirs)
	}
}

func TestPathToDirectionsEmpty(t *testing.T) {
	path := [][2]int{{100, 100}}
	dirs := PathToDirections(path)
	if len(dirs) != 0 {
		t.Errorf("len = %d, want 0", len(dirs))
	}
}

func TestDirectionKeys(t *testing.T) {
	tests := []struct {
		dir  Direction
		want []string
	}{
		{North, []string{"up"}},
		{Southeast, []string{"down", "right"}},
		{West, []string{"left"}},
	}
	for _, tt := range tests {
		got := tt.dir.Keys()
		if len(got) != len(tt.want) {
			t.Errorf("Direction(%v).Keys() = %v, want %v", tt.dir, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("Direction(%v).Keys()[%d] = %q, want %q", tt.dir, i, got[i], tt.want[i])
			}
		}
	}
}
