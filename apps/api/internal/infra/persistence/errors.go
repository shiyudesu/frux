package persistence

import (
	"errors"

	"gorm.io/gorm"
)

// IsDuplicatedKeyError reports translated unique-constraint violations.
func IsDuplicatedKeyError(err error) bool {
	return errors.Is(err, gorm.ErrDuplicatedKey)
}
