package handlers

import (
	"github.com/centrifugal/gocent/v3"
	"gorm.io/gorm"
)

type Env struct {
	DB               *gorm.DB
	CentrifugoClient *gocent.Client
}
