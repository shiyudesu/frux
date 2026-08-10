package domainembedding

import (
	"unicode"
	"unicode/utf8"
)

func BuildValidatedVideoText(title, description string) (string, error) {
	for _, value := range []string{title, description} {
		if !utf8.ValidString(value) {
			return "", ErrInvalidHashText
		}
		for _, character := range value {
			if unicode.IsControl(character) && !unicode.IsSpace(character) {
				return "", ErrInvalidHashText
			}
		}
	}
	return BuildVideoText(title, description), nil
}
