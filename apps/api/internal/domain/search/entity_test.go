package domainsearch

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizeQueryUsesUnicodeCodePointLimit(t *testing.T) {
	query, err := NormalizeQuery("  " + strings.Repeat("界", MaxQueryRunes) + "  ")
	if err != nil || len([]rune(query)) != MaxQueryRunes {
		t.Fatalf("64-rune query was rejected: query=%q err=%v", query, err)
	}
	if _, err := NormalizeQuery(strings.Repeat("界", MaxQueryRunes+1)); !errors.Is(err, ErrQueryTooLong) {
		t.Fatalf("65-rune query error = %v, want ErrQueryTooLong", err)
	}
	if _, err := NormalizeQuery("\u3000 \t"); !errors.Is(err, ErrEmptyQuery) {
		t.Fatalf("Unicode whitespace query error = %v, want ErrEmptyQuery", err)
	}
	if _, err := NormalizeQuery(string([]byte{0xff})); !errors.Is(err, ErrInvalidQuery) {
		t.Fatalf("invalid UTF-8 query error = %v, want ErrInvalidQuery", err)
	}
}

func TestEscapeLikeLiteralEscapesPostgreSQLWildcards(t *testing.T) {
	const input = `a\b%c_d`
	const want = `a\\b\%c\_d`
	if got := EscapeLikeLiteral(input); got != want {
		t.Fatalf("EscapeLikeLiteral(%q) = %q, want %q", input, got, want)
	}
}
