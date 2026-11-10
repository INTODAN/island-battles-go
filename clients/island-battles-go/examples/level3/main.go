// Command level3 is the default bot of level 3 as a program: level 2's
// attacks, plus defence, combined attacks it remembers in its memory, and
// speed upgrades. The bot itself is defaultbots.LevelThree.
//
//	go run ./examples/level3 check
//	go run ./examples/level3 serve
package main

import (
	islandbattles "github.com/INTODAN/island-battles-go/clients/island-battles-go"
	"github.com/INTODAN/island-battles-go/clients/island-battles-go/examples/defaultbots"
)

func main() {
	islandbattles.Main(defaultbots.LevelThree)
}
