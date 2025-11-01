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
	
	fmt.Println("Postgis extension enabled!")
	
	err = db.AutoMigrate(&models.Map{})
	if err != nil {
		log.Fatalln("failed to migrate map model")
	}
	err = db.AutoMigrate(&models.Castle{})
	if err != nil {
		log.Fatalln("failed to migrate castle model")
	}
	
	fmt.Println("migrated models!")
	return db
}
