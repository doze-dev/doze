package display

// The screen of a folder with no doze.hcl yet (Empty.dc.html): three ways to
// start, in the order they are offered.

// Choice is one way to start: its title and a line on what it does.
type Choice struct{ Title, What string }

// EmptyChoices are the three ways to start, in order: from what the project
// says, one named thing, or an empty file.
var EmptyChoices = [3]Choice{
	{"Set it up from this repo", "reads go.mod, package.json, compose files and writes a doze.hcl"},
	{"Add one thing", "name a module or a service: postgres, valkey, mongo, …"},
	{"Start with an empty file", "writes a short doze.hcl, with comments, to fill in"},
}

// EmptyNothingFound is what the first choice says instead when the project
// has nothing doze recognises.
const EmptyNothingFound = "nothing recognised here; it asks instead"
