package models

import "time"

// Role represents a permission role (Spatie-compatible)
type Role struct {
	ID          uint         `gorm:"primaryKey" json:"id"`
	Name        string       `gorm:"type:varchar(125);not null" json:"name"`
	GuardName   string       `gorm:"type:varchar(125);default:'api'" json:"guard_name"`
	CreatedAt   time.Time    `json:"created_at"`
	UpdatedAt   time.Time    `json:"updated_at"`
	Permissions []Permission `gorm:"many2many:role_has_permissions;joinForeignKey:role_id;joinReferences:permission_id" json:"permissions,omitempty"`
}

func (Role) TableName() string { return "roles" }

// Permission represents a granular permission
type Permission struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Name      string    `gorm:"type:varchar(125);not null" json:"name"`
	GuardName string    `gorm:"type:varchar(125);default:'api'" json:"guard_name"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (Permission) TableName() string { return "permissions" }

// RoleHasPermission is the pivot for role-permission assignments
type RoleHasPermission struct {
	PermissionID uint `gorm:"primaryKey"`
	RoleID       uint `gorm:"primaryKey"`
}

func (RoleHasPermission) TableName() string { return "role_has_permissions" }
