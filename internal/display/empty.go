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
const EmptyNothingFound = "nothing here it recognises; add one thing instead"

// StarterFile is what "start with an empty file" writes: the four nouns, and
// what each is for, with nothing declared. Both faces write the same one.
const StarterFile = `# doze.hcl: what this project runs, and what it needs.
#
#   app     "name" { run = "..." }          your code
#   module  postgres "db" { }              from the doze registry
#   service "name" { run = "..." }         anything else
#   task    "name" { run = "..." }         runs once, then gets out of the way
#
# doze add postgres   adds one for you.   doze up   starts it all.

stack = "my-project"
`
