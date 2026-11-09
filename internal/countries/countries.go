// Package countries is what api knows of a learner's country before the DPIA
// (R119 spec; R36): its form, and the countries no deployment may take learners from
// until the DPIA is done — those of the GDPR and of the UK GDPR. Which countries
// a deployment takes is the operator's list in the database, closed by default.
package countries

import "slices"

// Closed is the countries of the European Union, of the European Economic Area
// and the United Kingdom, by ISO 3166-1 alpha-2, with the parts of them that
// have codes of their own:
//   - the 27 of the EU, as "EU countries" names them
//     (https://european-union.europa.eu/principles-countries-history/eu-countries_en,
//     read 2026-11-09);
//   - the outermost regions of the EU with codes of their own: "five French
//     overseas departments – Martinique, Mayotte, Guadeloupe, French Guiana and
//     Réunion; one French overseas community – Saint-Martin"; Madeira, the Azores
//     and the Canary Islands have none (European Parliament, "Outermost regions
//     (ORs)", https://www.europarl.europa.eu/factsheets/en/sheet/100/outermost-regions-ors-,
//     read 2026-11-09);
//   - "the three EEA EFTA States – Iceland, Liechtenstein and Norway"
//     (https://www.efta.int/eea/eea-agreement, read 2026-11-09);
//   - the United Kingdom, under the UK GDPR (R119 spec);
//   - closed by caution, until the DPIA (R119 spec): the Åland Islands, a part
//     of Finland with a code of its own, and the codes that name one of these
//     countries otherwise — UK for GB, EL for GR, IC for the Canary Islands, EA
//     for Ceuta and Melilla.
var Closed = []string{
	"AT", "BE", "BG", "CY", "CZ", "DE", "DK", "EE", "ES", "FI", "FR", "GR", "HR", "HU",
	"IE", "IT", "LT", "LU", "LV", "MT", "NL", "PL", "PT", "RO", "SE", "SI", "SK",
	"MQ", "YT", "GP", "GF", "RE", "MF",
	"IS", "LI", "NO",
	"GB",
	"AX", "UK", "EL", "IC", "EA",
}

// IsClosed says whether country may not be opened before the DPIA.
func IsClosed(country string) bool { return slices.Contains(Closed, country) }

// Valid says whether country has the form of ISO 3166-1 alpha-2: two capital
// Latin letters. Whether such a country exists the list of the operator says.
func Valid(country string) bool {
	return len(country) == 2 && isUpper(country[0]) && isUpper(country[1])
}

func isUpper(b byte) bool { return b >= 'A' && b <= 'Z' }
