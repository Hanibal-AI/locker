package pii

import "strings"

// digitsOnly strips every non-digit rune, so a checksum can be computed
// regardless of how the number was grouped/spaced in the source text.
func digitsOnly(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// luhnValid implements the Luhn checksum. It backs both payment card
// validation and French SIREN/SIRET business identifiers, which use the
// same algorithm over a different digit count.
func luhnValid(digits string) bool {
	if digits == "" {
		return false
	}
	sum := 0
	alt := false
	for i := len(digits) - 1; i >= 0; i-- {
		d := int(digits[i] - '0')
		if alt {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
		alt = !alt
	}
	return sum%10 == 0
}

// cardLuhn validates a candidate payment card number: right length, and
// passes Luhn. Lengths that collide with SIREN (9) or SIRET (14) are
// excluded here — those are claimed by their own dedicated rules instead,
// see Docs/roadmap.md Phase 2.5 ("SIREN vs. credit card" false-positive
// trap).
func cardLuhn(raw string) bool {
	digits := digitsOnly(raw)
	n := len(digits)
	if n < 13 || n > 19 || n == 9 || n == 14 {
		return false
	}
	return luhnValid(digits)
}

// sirenValid validates a candidate French SIREN (9-digit business
// identifier): right length, and passes Luhn.
func sirenValid(raw string) bool {
	digits := digitsOnly(raw)
	return len(digits) == 9 && luhnValid(digits)
}

// siretValid validates a candidate French SIRET (14-digit establishment
// identifier: SIREN + 5-digit NIC): right length, and passes Luhn.
func siretValid(raw string) bool {
	digits := digitsOnly(raw)
	return len(digits) == 14 && luhnValid(digits)
}

// ibanValid implements the ISO 7064 MOD 97-10 checksum used by IBANs.
func ibanValid(raw string) bool {
	iban := strings.ToUpper(strings.ReplaceAll(raw, " ", ""))
	if len(iban) < 15 || len(iban) > 34 {
		return false
	}
	for _, r := range iban {
		if (r < '0' || r > '9') && (r < 'A' || r > 'Z') {
			return false
		}
	}

	rearranged := iban[4:] + iban[:4]
	remainder := 0
	for _, r := range rearranged {
		switch {
		case r >= '0' && r <= '9':
			remainder = (remainder*10 + int(r-'0')) % 97
		case r >= 'A' && r <= 'Z':
			remainder = (remainder*100 + int(r-'A') + 10) % 97
		}
	}
	return remainder == 1
}
