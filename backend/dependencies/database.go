package dependencies

import (
	"fmt"
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
	
	sql := "CREATE EXTENSION IF NOT EXISTS postgis;"
	if result := db.Exec(sql); result.Error != nil {
		log.Fatalf("Failed to create PostGIS extension: %v", result.Error)
	}
	
	fmt.Println("Postgis extension enabled!")
	
	db.AutoMigrate(&models.Map{})
	db.AutoMigrate(&models.Castle{})
	
	fmt.Println("migrated models!")
	return db
}
