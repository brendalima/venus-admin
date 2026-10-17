package rest

import (
	"errors"
	"strconv"
	"strings"
)

// Prices are typed in reais and stored in integer cents. This file is the
// only place that converts between the two.

var errInvalidPrice = errors.New("invalid price")

// maxPriceDigits caps the reais part well below int64's range, so no
// input can overflow the conversion.
const maxPriceDigits = 9

// parseReais reads a price the way people in Brazil write it: "189",
// "189,90", "1.234,56", "R$ 189,90". "189.90" is read as a decimal point
// too, since phone keyboards often offer only the dot. An empty field is
// zero, which the domain reports as a missing price.
func parseReais(s string) (int64, error) {
	s = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "R$"))
	s = strings.ReplaceAll(s, " ", "")
	if s == "" {
		return 0, nil
	}

	whole, frac := s, ""
	if i := strings.LastIndexByte(s, ','); i >= 0 {
		whole, frac = s[:i], s[i+1:]
	} else if strings.Count(s, ".") == 1 && len(s)-strings.IndexByte(s, '.') <= 3 {
		i := strings.IndexByte(s, '.')
		whole, frac = s[:i], s[i+1:]
	}

	whole, ok := withoutThousands(whole)
	if !ok || len(frac) > 2 || len(whole) > maxPriceDigits || !digitsOnly(frac) {
		return 0, errInvalidPrice
	}
	if whole == "" {
		whole = "0"
	}
	for len(frac) < 2 {
		frac += "0"
	}

	reais, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		return 0, errInvalidPrice
	}
	cents, err := strconv.ParseInt(frac, 10, 64)
	if err != nil {
		return 0, errInvalidPrice
	}
	return reais*100 + cents, nil
}

// formatReais writes cents the way parseReais reads them: 123456 becomes
// "1.234,56".
func formatReais(cents int64) string {
	sign := ""
	if cents < 0 {
		sign, cents = "-", -cents
	}
	whole := strconv.FormatInt(cents/100, 10)
	for i := len(whole) - 3; i > 0; i -= 3 {
		whole = whole[:i] + "." + whole[i:]
	}
	return sign + whole + "," + strconv.FormatInt(cents%100+100, 10)[1:]
}

// withoutThousands removes the dots from "1.234.567", and reports false
// when they are not where thousands separators go: a stray dot is more
// likely a typo than a price.
func withoutThousands(s string) (string, bool) {
	groups := strings.Split(s, ".")
	for i, g := range groups {
		if !digitsOnly(g) || (i > 0 && len(g) != 3) || (i == 0 && len(groups) > 1 && (g == "" || len(g) > 3)) {
			return "", false
		}
	}
	return strings.Join(groups, ""), true
}

func digitsOnly(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
