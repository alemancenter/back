package models

import "time"

type ContactMessage struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Name      string    `gorm:"type:varchar(255);not null" json:"name"`
	Email     string    `gorm:"type:varchar(255);not null" json:"email"`
	Phone     string    `gorm:"type:varchar(100)" json:"phone"`
	Subject   string    `gorm:"type:varchar(500);not null" json:"subject"`
	Message   string    `gorm:"type:text;not null" json:"message"`
	PageURL   string    `gorm:"type:varchar(1000)" json:"page_url"`
	Read      bool      `gorm:"default:false" json:"read"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (ContactMessage) TableName() string { return "contact_messages" }
