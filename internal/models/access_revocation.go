package models

import "time"

type AccessTokenRevocation struct {
	TokenHash string    `gorm:"type:char(64);primaryKey"`
	ExpiresAt time.Time `gorm:"not null;index"`
}

func (AccessTokenRevocation) TableName() string { return "access_token_revocations" }
