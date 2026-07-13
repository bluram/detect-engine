package main

import (
	"fmt"

	"github.com/bluram/advert-detect/internal/audio"
	"github.com/bluram/advert-detect/internal/deps"
)

func main() {
	deps.Verify()

	samples, err := audio.Decode("advert.mp3")
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	fmt.Println("samples amount:", len(samples))
	fmt.Println("first 10:", samples[:10])
}
