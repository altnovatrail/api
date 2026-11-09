package authz

// NewServer opens OpenFGA on the database at url for the tests, which reach into
// the store as no code of api does: they drop it, or write it another model.
var NewServer = newServer
