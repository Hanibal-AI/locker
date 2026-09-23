package ner

import (
	"regexp"
	"strings"
)

var monthNames = []string{
	"january", "february", "march", "april", "may", "june", "july",
	"august", "september", "october", "november", "december",
	"janvier", "fevrier", "février", "mars", "avril", "mai", "juin",
	"juillet", "aout", "août", "septembre", "octobre", "novembre",
	"decembre", "décembre",
}

var (
	// ISO 8601: 4-digit year first, so it can't collide with the
	// DD-MM-YYYY dash format below (which requires the year last).
	isoDatePattern = regexp.MustCompile(`\b\d{4}-\d{1,2}-\d{1,2}\b`)

	// Numeric EU/US style: 12/03/2024, 12.03.2024.
	numericDatePattern = regexp.MustCompile(`\b\d{1,2}[/.]\d{1,2}[/.]\d{2,4}\b`)

	// DD-MM-YYYY, e.g. 12-03-2024.
	dashDatePattern = regexp.MustCompile(`\b\d{1,2}-\d{1,2}-\d{4}\b`)

	// English-style "March 3, 2024" / "March 3rd 2024".
	monthDayYearPattern = regexp.MustCompile(`(?i)\b(?:` + strings.Join(monthNames, "|") + `)\s+\d{1,2}(?:st|nd|rd|th)?,?\s+\d{4}\b`)

	// French/European-style "3 mars 2024", also matches English "3 March 2024".
	dayMonthYearPattern = regexp.MustCompile(`(?i)\b\d{1,2}(?:st|nd|rd|th)?\s+(?:` + strings.Join(monthNames, "|") + `)\s+\d{4}\b`)
)

var datePatterns = []*regexp.Regexp{
	isoDatePattern,
	numericDatePattern,
	dashDatePattern,
	monthDayYearPattern,
	dayMonthYearPattern,
}

// recognizeDates finds explicit calendar dates by shape, in several
// common formats. It does not validate calendar correctness (e.g. it
// will match "31 February 2024") — see the package doc comment for scope.
// Overlaps between these patterns, or with a RegEx-layer match, are
// resolved by pii.Engine.Detect, not here.
func recognizeDates(text string) []Entity {
	var entities []Entity
	for _, pattern := range datePatterns {
		for _, loc := range pattern.FindAllStringIndex(text, -1) {
			entities = append(entities, Entity{
				Type:  TypeDate,
				Value: text[loc[0]:loc[1]],
				Start: loc[0],
				End:   loc[1],
			})
		}
	}
	return entities
}
