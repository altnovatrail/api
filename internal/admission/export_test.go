package admission

import "github.com/altnovatrail/tutor"

// TutorOf is the tutor the gate g asks, so that a test compares the gate's
// answer with tutor's own choice.
func TutorOf(g *Gate) *tutor.Tutor { return g.tutor }

// GateOn is the gate that asks t, a tutor of the test's own configuration —
// adapters api does not give among them —, with the providers of the table.
func GateOn(t *tutor.Tutor) (*Gate, error) {
	providers, err := tableProviders()
	return &Gate{tutor: t, providers: providers}, err
}

// ReadProviders reads the providers of a table of the test's own.
var ReadProviders = readProviders
