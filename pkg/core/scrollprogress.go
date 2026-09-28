package core

import (
	"fmt"
	"hash/fnv"
)

// scrollStuckWindow is how many consecutive captures the stuck check keeps.
// Four is enough to see a rubber-band bounce (A, B, A, B) whole.
const scrollStuckWindow = 4

// scrollStuckRepeats is how many identical consecutive captures mean the
// surface has stopped: three captures are two scrolls that changed nothing.
// One no-change scroll is not proof — the loop settles for a fixed 300 ms,
// and a slow device can render a fling after that.
const scrollStuckRepeats = 3

// ScrollProgress decides when a scrollUntilVisible loop should stop because
// the surface it is scrolling no longer moves.
//
// Without it, a target that is not in the list at all costs every scroll the
// step allows: Maestro loops to its timeout, and so did every driver here.
// A container at its end, or one bouncing off its edge, keeps reporting the
// same content; that is what the loop reads instead of asking the platform
// whether a scroll "worked", which no driver can answer reliably.
//
// Two shapes count as stuck, adapted from agent-device's scroll runtime:
// the last three captures identical (the end of the content), or the last
// four alternating between two signatures (a bounce off an edge). A, A, A, B
// is a list that just moved and keeps going. A capture the driver could not
// read is never observed, so an unreadable screen cannot be mistaken for
// the end of the content.
type ScrollProgress struct {
	recent []string
}

// Observe records the signature of the scroll surface as it stands before the
// next scroll and reports whether the surface has stopped moving.
func (p *ScrollProgress) Observe(signature string) bool {
	p.recent = append(p.recent, signature)
	if len(p.recent) > scrollStuckWindow {
		p.recent = p.recent[len(p.recent)-scrollStuckWindow:]
	}
	n := len(p.recent)
	if n >= scrollStuckRepeats {
		held := true
		for _, s := range p.recent[n-scrollStuckRepeats:] {
			if s != p.recent[n-1] {
				held = false
				break
			}
		}
		if held {
			return true
		}
	}
	if n == scrollStuckWindow {
		a, b := p.recent[0], p.recent[1]
		if a != b && p.recent[2] == a && p.recent[3] == b {
			return true
		}
	}
	return false
}

// ScrollSignature reduces a capture of the scroll surface (page source, a
// serialised snapshot) to a short key for ScrollProgress. Hashing the whole
// capture keeps bounds in the comparison, so a list that advanced by one row
// with the same set of labels still reads as movement.
func ScrollSignature(capture string) string {
	h := fnv.New64a()
	_, _ = h.Write([]byte(capture))
	return fmt.Sprintf("%016x", h.Sum64())
}
