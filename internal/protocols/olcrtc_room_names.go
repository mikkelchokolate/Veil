package protocols

import (
	"crypto/rand"
	"math/big"
	"strings"
)

// Jitsi's own UI suggests room names by concatenating capitalised words
// (adjective + plural noun + verb + adverb), e.g. "HappyTigersRunSwiftly".
// Generating rooms in exactly that shape makes an auto-provisioned room
// indistinguishable from one a person created in the Jitsi UI — there is no
// "veil"/panel marker or obviously-automated pattern in the URL.
var (
	olcrtcRoomAdjectives = []string{
		"Happy", "Brave", "Calm", "Clever", "Bright", "Gentle", "Bold", "Quiet",
		"Swift", "Lucky", "Mighty", "Noble", "Proud", "Witty", "Eager", "Fancy",
		"Jolly", "Kind", "Lively", "Merry", "Polite", "Quick", "Royal", "Shiny",
		"Sunny", "Warm", "Wise", "Young", "Cosmic", "Golden", "Silent", "Velvet",
	}
	olcrtcRoomNouns = []string{
		"Tigers", "Pandas", "Eagles", "Foxes", "Whales", "Otters", "Falcons",
		"Dragons", "Wolves", "Lions", "Bears", "Hawks", "Dolphins", "Rabbits",
		"Horses", "Comets", "Rivers", "Mountains", "Forests", "Gardens",
		"Beacons", "Lanterns", "Harbors", "Meadows", "Glaciers", "Canyons",
		"Willows", "Maples", "Cedars", "Orchids", "Sparrows", "Ravens",
	}
	olcrtcRoomVerbs = []string{
		"Run", "Jump", "Dance", "Sing", "Dream", "Build", "Glide", "Wander",
		"Gather", "Explore", "Wonder", "Travel", "Sparkle", "Flourish",
		"Whisper", "Shine", "Drift", "Soar", "Bloom", "Gleam", "Roam", "Leap",
	}
	olcrtcRoomAdverbs = []string{
		"Quietly", "Boldly", "Gently", "Swiftly", "Brightly", "Calmly",
		"Freely", "Happily", "Kindly", "Smoothly", "Warmly", "Wisely",
		"Eagerly", "Gracefully", "Proudly", "Quickly", "Softly", "Bravely",
	}
)

// jitsiStyleRoomName returns a natural Jitsi-style room name like
// "BraveLionsThinkLoudly" using a cryptographically random word from each list.
func jitsiStyleRoomName() (string, error) {
	var b strings.Builder
	for _, list := range [][]string{olcrtcRoomAdjectives, olcrtcRoomNouns, olcrtcRoomVerbs, olcrtcRoomAdverbs} {
		idx, err := cryptoIntn(len(list))
		if err != nil {
			return "", err
		}
		b.WriteString(list[idx])
	}
	return b.String(), nil
}

func cryptoIntn(n int) (int, error) {
	r, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		return 0, err
	}
	return int(r.Int64()), nil
}
