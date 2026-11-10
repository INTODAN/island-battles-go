// Command level2 is the default bot of level 2 as a program: each of its
// castles attacks the nearest castle it can take, forecasting the defenders
// when its group lands. The bot itself is defaultbots.LevelTwo.
//
//	go run ./examples/level2 check
//	go run ./examples/level2 serve
package main

import (
	islandbattles "github.com/INTODAN/island-battles-go/clients/island-battles-go"
	"github.com/INTODAN/island-battles-go/clients/island-battles-go/examples/defaultbots"
)

func main() {
	islandbattles.Main(defaultbots.LevelTwo)
}
