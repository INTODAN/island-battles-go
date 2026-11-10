// Command level1 is the default bot of level 1 as a program: each turn it
// sends one soldier from its first castle with soldiers to the first enemy
// castle. The bot itself is defaultbots.LevelOne.
//
//	go run ./examples/level1 check
//	go run ./examples/level1 serve
package main

import (
	islandbattles "github.com/INTODAN/island-battles-go/clients/island-battles-go"
	"github.com/INTODAN/island-battles-go/clients/island-battles-go/examples/defaultbots"
)

func main() {
	islandbattles.Main(defaultbots.LevelOne)
}
