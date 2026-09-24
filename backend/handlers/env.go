package handlers

import "gorm.io/gorm"

type Env struct {
	DB *gorm.DB
}
