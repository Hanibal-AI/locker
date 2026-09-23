package ner

// These lists are intentionally small and curated, not exhaustive — they
// exist to disambiguate, not to be a complete directory of every name,
// city, or company in existence. Precision (few false positives) is
// preferred over recall here; see the package doc comment for why.

// firstNames biases a capitalized run's first token toward PERSON, common
// French and English given names.
var firstNames = buildSet([]string{
	"jean", "pierre", "michel", "philippe", "alain", "bernard", "nicolas",
	"laurent", "julien", "thomas", "antoine", "olivier", "sebastien",
	"francois", "david", "marc", "christophe", "stephane", "vincent",
	"marie", "nathalie", "isabelle", "sylvie", "catherine", "sophie",
	"martine", "anne", "julie", "celine", "camille", "claire", "emilie",
	"charlotte", "laura", "sarah", "alice",
	"john", "james", "robert", "william", "richard", "charles", "daniel",
	"matthew", "andrew", "joshua", "kevin", "brian", "george", "edward",
	"mary", "patricia", "jennifer", "linda", "elizabeth", "susan",
	"jessica", "karen", "sandra", "emma", "olivia", "sophia", "grace",
})

// placeNames biases a capitalized run toward LOC on an exact gazetteer
// hit, taking priority over an ambiguous trigger word.
var placeNames = buildSet([]string{
	"paris", "lyon", "marseille", "toulouse", "nice", "nantes",
	"strasbourg", "bordeaux", "lille", "rennes", "reims", "toulon",
	"boulogne", "boulogne-billancourt", "versailles", "nanterre",
	"montpellier", "grenoble", "dijon", "angers", "nimes", "le havre",
	"saint-etienne", "clermont-ferrand",
	"london", "berlin", "madrid", "rome", "amsterdam", "brussels",
	"geneva", "zurich", "vienna", "dublin", "lisbon", "new york",
	"san francisco", "los angeles", "chicago", "boston", "seattle",
	"tokyo", "beijing", "shanghai", "singapore", "sydney", "toronto",
})

// titles preceding a capitalized run mark it as PERSON regardless of any
// other signal.
var titles = buildSet([]string{
	"m", "mme", "mlle", "dr", "pr", "me", // French: M., Mme, Mlle, Dr, Pr, Me
	"mr", "mrs", "ms", "miss", "prof",
})

// personTriggers preceding a capitalized run mark it as PERSON.
var personTriggers = buildSet([]string{
	"with", "avec", "contact", "contacter", "appele", "appelee",
	"nomme", "nommee", "signe", "signee", "par",
})

// orgSuffixes immediately following a capitalized run mark it as ORG
// (the suffix is included in the matched span).
var orgSuffixes = buildSet([]string{
	"sa", "sas", "sasu", "sarl", "eurl", "sci", "group", "groupe",
	"inc", "llc", "ltd", "corp", "corporation", "gmbh", "ag", "co",
})

// orgTriggers preceding a capitalized run mark it as ORG, unless the run
// is an exact placeNames hit (checked first) or a locTrigger applies.
var orgTriggers = buildSet([]string{"chez", "at"})

// locTriggers preceding a capitalized run mark it as LOC. Bare "a" is
// deliberately excluded even though it can mean "to" in some contexts —
// it collides with the common French verb form "a" ("il a ..."), which
// would otherwise false-trigger LOC too often.
var locTriggers = buildSet([]string{"à", "in", "from", "near", "vers"})

func buildSet(words []string) map[string]struct{} {
	set := make(map[string]struct{}, len(words))
	for _, w := range words {
		set[w] = struct{}{}
	}
	return set
}
