package dependencies

import (
	"fmt"
	"islandBattles/backend/models"
	"log"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

const DatabaseURL = "postgres://islandBattles:islandBattles@localhost:5432/islandBattles"

func InitDB(connectionString string) *gorm.DB {
	db, err := gorm.Open(postgres.Open(connectionString), &gorm.Config{})
	if err != nil {
		log.Fatalln("failed to connect database")
	}

	err = db.AutoMigrate(&models.Map{}, &models.Castle{}, &models.CastleTemplate{}, &models.MapTemplate{},
		&models.Generation{}, &models.SoldierGroup{})
	if err != nil {
		log.Fatalln("failed to migrate models")
	}

	fmt.Println("migrated models!")
	return db
}
