package report

import (
	"strconv"
	"strings"
)

// usd formats a measured dollar figure with enough precision to be honest
// about small per-1k costs (0.0475) without inventing digits.
func usd(f float64) string {
	switch {
	case f == 0:
		return "$0"
	case f >= 100:
		return "$" + commas(strconv.FormatFloat(f, 'f', 0, 64))
	case f >= 1:
		return "$" + strconv.FormatFloat(f, 'f', 2, 64)
	default:
		return "$" + strconv.FormatFloat(f, 'g', 3, 64)
	}
}

func commas(s string) string {
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

func ms(f float64) string {
	if f >= 100 {
		return strconv.FormatFloat(f, 'f', 0, 64) + " ms"
	}
	return strconv.FormatFloat(f, 'f', 1, 64) + " ms"
}

func float1(f float64) string { return strconv.FormatFloat(f, 'f', 1, 64) }

func points(p *float64) string {
	if p == nil {
		return "—"
	}
	return strconv.FormatFloat(*p, 'f', 1, 64)
}

func gradeLabel(g string) string {
	if g == "insufficient_evidence" {
		return "no grade"
	}
	if g == "" {
		return "—"
	}
	return g
}

func title(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func sourceName(s string) string {
	switch s {
	case "tokvera":
		return "Tokvera"
	case "sievant":
		return "Sievant"
	}
	return s
}

// impactLabel is the short "how bad" column for an item.
func impactLabel(severity, status string, impact float64) string {
	if severity != "" {
		return severity
	}
	return status + ", −" + strconv.FormatFloat(impact, 'f', -1, 64) + " pts"
}
