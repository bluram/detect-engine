package main

import (
	"fmt"

	"github.com/bluram/advert-detect/internal/deps"
)

func main() {
	deps.Verify()
	fmt.Println("ffmpeg на месте, можно работать!")
}
