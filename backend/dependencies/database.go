package dependencies

import (
	"islandBattles/backend/models"
	"log"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

const databaseURL = "postgres://islandBattles:islandBattles@localhost:5432/islandBattles"

func InitDB() *gorm.DB {
	db, err := gorm.Open(postgres.Open(databaseURL), &gorm.Config{})
	if err != nil {
		log.Fatalln("failed to connect database")
	}
	db.AutoMigrate(&models.Castle{})
	return db
}
