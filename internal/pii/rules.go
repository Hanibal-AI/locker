package pii

import "regexp"

// Entity type identifiers used as both the Match.Type and the placeholder
// prefix (e.g. a masked email becomes "[EMAIL_1]").
const (
	TypeEmail      = "EMAIL"
	TypePhone      = "PHONE"
	TypeIBAN       = "IBAN"
	TypeCreditCard = "CB"
	TypeSIREN      = "SIREN"
	TypeSIRET      = "SIRET"
)

var (
	emailPattern = regexp.MustCompile(`[a-zA-Z0-9.!#$%&'*+/=?^_` + "`" + `{|}~-]+@[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(?:\.[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)+`)

	// French phone numbers: 0 or +33/0033 followed by 9 digits, grouped by 2.
	phonePattern = regexp.MustCompile(`\b(?:(?:\+33|0033)[ .-]?|0)[1-9](?:[ .-]?\d{2}){4}\b`)

	// Broad IBAN shape: 2 letters, 2 check digits, then grouped alphanumerics.
	// ibanValid() carries the real validation via the MOD 97-10 checksum.
	ibanPattern = regexp.MustCompile(`\b[A-Z]{2}\d{2}(?:[ ]?[A-Z0-9]{2,4}){3,8}\b`)

	// SIRET: 14 digits, conventionally grouped 3-3-3-5.
	siretPattern = regexp.MustCompile(`\b\d{3}[ ]?\d{3}[ ]?\d{3}[ ]?\d{5}\b`)

	// SIREN: 9 digits, conventionally grouped 3-3-3.
	sirenPattern = regexp.MustCompile(`\b\d{3}[ ]?\d{3}[ ]?\d{3}\b`)

	// Generic payment card shape: 13-19 digits, optionally grouped by
	// spaces or dashes. cardLuhn() does the real validation.
	cardPattern = regexp.MustCompile(`\b(?:\d[ -]?){12,18}\d\b`)
)

type rule struct {
	typ      string
	pattern  *regexp.Regexp
	validate func(raw string) bool // nil means "shape match is enough"
}

// builtinRules lists the built-in rules in priority order: rules earlier
// in the list are preferred when two candidates start at the same
// position (see Engine.Detect). IBAN/SIRET/SIREN/card all key off digit
// runs, so the more specific, longer patterns are listed first.
func builtinRules() []rule {
	return []rule{
		{typ: TypeIBAN, pattern: ibanPattern, validate: ibanValid},
		{typ: TypeSIRET, pattern: siretPattern, validate: siretValid},
		{typ: TypeSIREN, pattern: sirenPattern, validate: sirenValid},
		{typ: TypeCreditCard, pattern: cardPattern, validate: cardLuhn},
		{typ: TypeEmail, pattern: emailPattern, validate: nil},
		{typ: TypePhone, pattern: phonePattern, validate: nil},
	}
}

// builtinRuleName maps a built-in Type back to the config.yaml
// `enabled_rules` name used to select it.
func builtinRuleName(typ string) string {
	switch typ {
	case TypeEmail:
		return "email"
	case TypePhone:
		return "phone"
	case TypeIBAN:
		return "iban"
	case TypeCreditCard:
		return "credit_card"
	case TypeSIREN:
		return "siren"
	case TypeSIRET:
		return "siret"
	default:
		return ""
	}
}
