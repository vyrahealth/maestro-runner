package wda

import "os"

// hostedFrames reports whether page-source frames inside a view hosted by another process are
// moved onto the screen's coordinates (MAESTRO_WDA_HOSTED_FRAMES).
//
// On iOS 27 XCUITest reports the elements of a remotely hosted view in that view's own
// coordinate space. Apple Health's authorization sheet sits 48 points down on an iPhone 11 and
// every element inside it reports a frame 48 points too high, so a tap at an element's centre
// lands on whatever is drawn 48 points above it: "Select All 12 Topics" was tapped on the
// sheet's subtitle, and nothing was selected. A coordinate tap, a tap from a WDA rect and
// XCUITest's own element click all missed the same way, because all three use that frame.
//
// The page source shows where such a space starts: a child at (0,0) with exactly its parent's
// size, inside a parent that is not at the origin. An element in the screen's coordinates
// that fills its parent reports the parent's origin instead. With this switch set,
// ParsePageSource adds the parent's origin to that child and to everything under it.
//
// Two limits. Only lookups through the page source see the correction: a selector WDA answers
// with a query of its own (plain text, or an id that means itself) still gets WDA's rect, so a
// flow reaches a hosted view with a regex text or an id with its dots escaped. And a table cell
// that is off screen reports its children's frames relative to the cell, which no rule here can
// tell from a frame on the screen, so a flow scrolls the cell into view before it reads them.
func hostedFrames() bool {
	return os.Getenv("MAESTRO_WDA_HOSTED_FRAMES") != ""
}

// rebaseHostedFrames adds (offsetX, offsetY) to elem's frame, the offset its coordinate space
// needs, and walks its children, starting a new offset wherever a child begins a hosted space.
func rebaseHostedFrames(elem *ParsedElement, offsetX, offsetY int) {
	reportedX, reportedY := elem.Bounds.X, elem.Bounds.Y
	elem.Bounds.X += offsetX
	elem.Bounds.Y += offsetY
	for _, child := range elem.Children {
		childX, childY := offsetX, offsetY
		if startsHostedSpace(reportedX, reportedY, elem, child) {
			childX, childY = elem.Bounds.X, elem.Bounds.Y
		}
		rebaseHostedFrames(child, childX, childY)
	}
}

// startsHostedSpace reports whether child restarts the coordinates: it is at (0,0) with its
// parent's size, while the parent, as reported, is not at (0,0). Zero-size elements never do.
func startsHostedSpace(parentX, parentY int, parent, child *ParsedElement) bool {
	if parentX == 0 && parentY == 0 {
		return false
	}
	c := child.Bounds
	return c.X == 0 && c.Y == 0 && c.Width > 0 && c.Height > 0 &&
		c.Width == parent.Bounds.Width && c.Height == parent.Bounds.Height
}
