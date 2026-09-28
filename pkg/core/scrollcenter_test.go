package core

import "testing"

// On a 390x844 screen the lines are 422+168=590 (down), 422-168=254 (up),
// 195+78=273 (right) and 195-78=117 (left), and the comparisons are strict,
// as in UiElement.isElementNearScreenCenter.
func TestNearScreenCenterUsesMaestrosLines(t *testing.T) {
	const w, h = 390, 844
	row := func(centerY int) Bounds { return Bounds{X: 20, Y: centerY - 25, Width: 350, Height: 50} }
	col := func(centerX int) Bounds { return Bounds{X: centerX - 25, Y: 400, Width: 50, Height: 50} }
	cases := []struct {
		direction string
		b         Bounds
		want      bool
	}{
		{"down", row(589), true},
		{"down", row(590), false},
		{"down", row(100), true},
		{"up", row(255), true},
		{"up", row(254), false},
		{"up", row(800), true},
		{"right", col(272), true},
		{"right", col(273), false},
		{"left", col(118), true},
		{"left", col(117), false},
		{"diagonal", row(422), false},
	}
	for _, c := range cases {
		if got := NearScreenCenter(c.b, c.direction, w, h); got != c.want {
			cx, cy := c.b.Center()
			t.Errorf("%s, center (%d, %d): got %v, want %v", c.direction, cx, cy, got, c.want)
		}
	}
}

func TestScreenVisibleFractionCountsAScreenFillingElementAsVisible(t *testing.T) {
	const w, h = 390, 844
	tall := Bounds{X: 0, Y: -500, Width: 390, Height: 3000}
	if got := ScreenVisibleFraction(tall, w, h); got != 1 {
		t.Errorf("an element covering the screen: got %v, want 1 (VisibleFraction says %v)", got, VisibleFraction(tall, w, h))
	}
	half := Bounds{X: 50, Y: 794, Width: 290, Height: 100}
	if got := ScreenVisibleFraction(half, w, h); got != 0.5 {
		t.Errorf("half below the fold: got %v, want 0.5", got)
	}
	if got := ScreenVisibleFraction(Bounds{X: 10, Y: 10}, w, h); got != 0 {
		t.Errorf("a zero-size element: got %v, want 0", got)
	}
}
