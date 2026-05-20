package services

import (
	"errors"

	"gorm.io/gorm"
)

// ErrNotFound is returned when a requested record is not found in the database.
var ErrNotFound = errors.New("record not found")

// ErrForbidden is returned when the caller lacks ownership of the target resource.
var ErrForbidden = errors.New("forbidden")

// MapError translates data layer errors to service layer errors.
func MapError(err error) error {
	if err == gorm.ErrRecordNotFound {
		return ErrNotFound
	}
	return err
}

func MapErr0(err error) error {
	return MapError(err)
}

func MapErr1[T any](v T, err error) (T, error) {
	return v, MapError(err)
}

func MapErr2[T1 any, T2 any](v1 T1, v2 T2, err error) (T1, T2, error) {
	return v1, v2, MapError(err)
}

func MapErr3[T1 any, T2 any, T3 any](v1 T1, v2 T2, v3 T3, err error) (T1, T2, T3, error) {
	return v1, v2, v3, MapError(err)
}
