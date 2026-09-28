package core

// CenterElementRetries is how many times scrollUntilVisible with
// centerElement looks for its element near the middle of the screen before
// it settles for the plain visibility test. It is Maestro's
// maxRetryCenterCount (Orchestra.kt:800), and Maestro tries while its count
// is at most this, so that is five looks, each followed by a scroll.
const CenterElementRetries = 4

// CenterElementMinVisible is the share of an element that must be on screen
// before centerElement tries to center it. Maestro compares with ">"
// (Orchestra.kt:814); at or below it the plain visibility test decides.
const CenterElementMinVisible = 0.1

// ScreenVisibleFraction is VisibleFraction as Maestro's
// UiElement.getVisiblePercentage computes it (UiElement.kt:31-47): an element
// that covers the whole screen counts as fully visible, however far it runs
// past the edges. centerElement's 10% test uses it, so a tall list or a
// full-screen container is centered like Maestro centers it.
func ScreenVisibleFraction(b Bounds, screenW, screenH int) float64 {
	if b.Width == 0 && b.Height == 0 {
		return 0
	}
	if b.X <= 0 && b.Y <= 0 && b.X+b.Width >= screenW && b.Y+b.Height >= screenH {
		return 1
	}
	return VisibleFraction(b, screenW, screenH)
}

// NearScreenCenter reports whether an element scrolled into view counts as
// centered for scrollUntilVisible's centerElement, given the direction the
// step scrolls.
//
// This is Maestro's UiElement.isElementNearScreenCenter (UiElement.kt:49-68),
// with the same integer arithmetic. Maestro calls it with the swipe
// direction, the opposite of the scroll direction (ScrollDirection.kt:10-15).
// The element's center must have come past a line 20% of the screen beyond
// the middle, so it sits in the 70% of the screen the content is moving
// towards: scrolling down, its center must be above 70% of the height.
func NearScreenCenter(b Bounds, scrollDirection string, screenW, screenH int) bool {
	cx, cy := b.Center()
	switch scrollDirection {
	case "down": // Maestro's SwipeDirection.UP
		return cy < screenH/2+screenH/5
	case "up": // SwipeDirection.DOWN
		return cy > screenH/2-screenH/5
	case "right": // SwipeDirection.LEFT
		return cx < screenW/2+screenW/5
	case "left": // SwipeDirection.RIGHT
		return cx > screenW/2-screenW/5
	}
	return false
}
