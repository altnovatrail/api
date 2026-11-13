package admission

import "github.com/altnovatrail/tutor"

// TutorOf is the tutor the gate g asks, so that a test compares the gate's
// answer with tutor's own choice.
func TutorOf(g *Gate) *tutor.Tutor { return g.tutor }
