package models

import (
	"time"

	"golang.org/x/crypto/bcrypt"
)

// User represents an application user stored in the Jordan (main) database
type User struct {
	ID                     uint       `gorm:"primaryKey" json:"id"`
	Name                   string     `gorm:"type:varchar(255);not null" json:"name"`
	Email                  string     `gorm:"type:varchar(255);unique;not null" json:"email"`
	EmailVerifiedAt        *time.Time `json:"email_verified_at,omitempty"`
	Password               string     `gorm:"type:varchar(255);not null" json:"-"`
	GoogleID               *string    `gorm:"type:varchar(255)" json:"google_id,omitempty"`
	FacebookID             *string    `gorm:"type:varchar(255)" json:"facebook_id,omitempty"`
	Phone                  *string    `gorm:"type:varchar(50)" json:"phone,omitempty"`
	JobTitle               *string    `gorm:"type:varchar(255)" json:"job_title,omitempty"`
	Gender                 *string    `gorm:"type:enum('male','female','other')" json:"gender,omitempty"`
	Country                *string    `gorm:"type:varchar(100)" json:"country,omitempty"`
	Bio                    *string    `gorm:"type:text" json:"bio,omitempty"`
	SocialLinks            *string    `gorm:"type:json" json:"social_links,omitempty"`
	ProfilePhotoPath       *string    `gorm:"type:varchar(500)" json:"profile_photo_path,omitempty"`
	Status                 string     `gorm:"type:enum('active','inactive','banned');default:'active'" json:"status"`
	EmailBounceStatus      string     `gorm:"type:varchar(30);not null;default:'active'" json:"email_bounce_status"`
	EmailBounceCount       int        `gorm:"not null;default:0" json:"email_bounce_count"`
	EmailLastBounceAt      *time.Time `json:"email_last_bounce_at,omitempty"`
	EmailBounceReason      *string    `gorm:"type:text" json:"email_bounce_reason,omitempty"`
	LastActivity           *time.Time `json:"last_activity,omitempty"`
	LastSeen               *time.Time `json:"last_seen,omitempty"`
	RememberToken          *string    `gorm:"type:varchar(100)" json:"-"`
	TwoFactorSecret        *string    `gorm:"type:text" json:"-"`
	TwoFactorRecoveryCodes *string    `gorm:"type:text" json:"-"`
	TwoFactorConfirmedAt   *time.Time `json:"-"`
	CreatedAt              time.Time  `json:"created_at"`
	UpdatedAt              time.Time  `json:"updated_at"`

	// Relationships
	Roles       []Role       `gorm:"many2many:model_has_roles;joinForeignKey:model_id;joinReferences:role_id" json:"roles,omitempty"`
	Permissions []Permission `gorm:"many2many:model_has_permissions;joinForeignKey:model_id;joinReferences:permission_id" json:"permissions,omitempty"`
	PushTokens  []PushToken  `gorm:"foreignKey:UserID" json:"push_tokens,omitempty"`
}

func (User) TableName() string { return "users" }

// HashPassword hashes the user password
func (u *User) HashPassword(password string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	u.Password = string(hash)
	return nil
}

// CheckPassword verifies the password
func (u *User) CheckPassword(password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(u.Password), []byte(password)) == nil
}

// IsAdmin checks if user has admin role
func (u *User) IsAdmin() bool {
	for _, role := range u.Roles {
		if role.Name == "Admin" || role.Name == "Super Admin" {
			return true
		}
	}
	return false
}

// IsActive checks if user account is active
func (u *User) IsActive() bool {
	return u.Status != "inactive" && u.Status != "banned"
}

// CanReceiveEmail returns false when the address is permanently blocked from receiving mail.
func (u *User) CanReceiveEmail() bool {
	switch u.EmailBounceStatus {
	case "hard_bounce", "invalid_email", "unsubscribed":
		return false
	default:
		return true
	}
}

// IsVerified checks if email is verified
func (u *User) IsVerified() bool {
	return u.EmailVerifiedAt != nil
}

// IsOnline checks if user was active in the last 5 minutes
func (u *User) IsOnline() bool {
	if u.LastActivity == nil {
		return false
	}
	return time.Since(*u.LastActivity) < 5*time.Minute
}

// GetProfilePhotoURL returns the absolute URL for the profile photo
func (u *User) GetProfilePhotoURL(storageURL string) string {
	if u.ProfilePhotoPath == nil || *u.ProfilePhotoPath == "" {
		return ""
	}
	return storageURL + "/" + *u.ProfilePhotoPath
}

// HasPermission checks if user has a specific permission (via roles or direct)
func (u *User) HasPermission(permission string) bool {
	for _, p := range u.Permissions {
		if p.Name == permission {
			return true
		}
	}
	for _, r := range u.Roles {
		for _, p := range r.Permissions {
			if p.Name == permission {
				return true
			}
		}
	}
	return false
}

// HasRole checks if user has a specific role
func (u *User) HasRole(role string) bool {
	for _, r := range u.Roles {
		if r.Name == role {
			return true
		}
	}
	return false
}

// ModelHasRole is the pivot table for user-role assignments
type ModelHasRole struct {
	RoleID    uint   `gorm:"primaryKey"`
	ModelType string `gorm:"primaryKey;type:varchar(255)"`
	ModelID   uint   `gorm:"primaryKey"`
}

func (ModelHasRole) TableName() string { return "model_has_roles" }

// ModelHasPermission is the pivot table for user-permission assignments
type ModelHasPermission struct {
	PermissionID uint   `gorm:"primaryKey"`
	ModelType    string `gorm:"primaryKey;type:varchar(255)"`
	ModelID      uint   `gorm:"primaryKey"`
}

func (ModelHasPermission) TableName() string { return "model_has_permissions" }

// PersonalAccessToken represents a Sanctum-compatible API token
type PersonalAccessToken struct {
	ID            uint       `gorm:"primaryKey" json:"id"`
	TokenableType string     `gorm:"type:varchar(255);not null" json:"-"`
	TokenableID   uint       `gorm:"not null" json:"-"`
	Name          string     `gorm:"type:varchar(255);not null" json:"name"`
	Token         string     `gorm:"type:varchar(64);unique;not null" json:"-"`
	Abilities     *string    `gorm:"type:text" json:"abilities,omitempty"`
	LastUsedAt    *time.Time `json:"last_used_at,omitempty"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

func (PersonalAccessToken) TableName() string { return "personal_access_tokens" }

// PushToken represents an FCM/OneSignal push notification token
type PushToken struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	UserID    uint      `gorm:"not null" json:"user_id"`
	Token     string    `gorm:"type:varchar(500);not null" json:"token"`
	Platform  string    `gorm:"type:enum('fcm','onesignal','apns','web');default:'fcm'" json:"platform"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (PushToken) TableName() string { return "push_tokens" }
