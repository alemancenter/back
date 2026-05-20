package models

import (
	"testing"
	"time"
)

func TestUser_Password(t *testing.T) {
	u := &User{}
	err := u.HashPassword("mysecretpassword")
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}

	if u.Password == "" || u.Password == "mysecretpassword" {
		t.Errorf("Password was not hashed properly")
	}

	if !u.CheckPassword("mysecretpassword") {
		t.Errorf("CheckPassword failed for correct password")
	}

	if u.CheckPassword("wrongpassword") {
		t.Errorf("CheckPassword succeeded for wrong password")
	}
}

func TestUser_IsAdmin(t *testing.T) {
	u1 := &User{Roles: []Role{{Name: "User"}}}
	if u1.IsAdmin() {
		t.Errorf("Expected IsAdmin to be false for regular user")
	}

	u2 := &User{Roles: []Role{{Name: "Admin"}}}
	if !u2.IsAdmin() {
		t.Errorf("Expected IsAdmin to be true for Admin")
	}
}

func TestUser_IsActive(t *testing.T) {
	u1 := &User{Status: "active"}
	if !u1.IsActive() {
		t.Errorf("Expected active user to be active")
	}

	u2 := &User{Status: "banned"}
	if u2.IsActive() {
		t.Errorf("Expected banned user to be inactive")
	}
}

func TestUser_IsOnline(t *testing.T) {
	now := time.Now()
	u1 := &User{LastActivity: &now}
	if !u1.IsOnline() {
		t.Errorf("Expected user active now to be online")
	}

	past := now.Add(-10 * time.Minute)
	u2 := &User{LastActivity: &past}
	if u2.IsOnline() {
		t.Errorf("Expected user active 10 mins ago to be offline")
	}
}
