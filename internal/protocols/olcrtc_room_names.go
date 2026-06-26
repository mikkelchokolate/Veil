package protocols

import (
	"crypto/rand"
	"encoding/hex"
	"math/big"
	"strings"
)

// Real meeting rooms people create have wildly varying names: Jitsi-style word
// combos, short hex/ids, words with a number tacked on, dash-separated phrases,
// etc. If every auto-provisioned room followed one fixed pattern (e.g. always
// four CamelCase words) that pattern would itself become a fingerprint across
// the many users of a panel. So we pick a random *format scheme* per room and
// draw from sizable word lists — the resulting links share no single shape and
// none carry a veil/panel/tool marker.
var (
	olcrtcRoomAdjectives = []string{
		"Happy", "Brave", "Calm", "Clever", "Bright", "Gentle", "Bold", "Quiet",
		"Swift", "Lucky", "Mighty", "Noble", "Proud", "Witty", "Eager", "Fancy",
		"Jolly", "Kind", "Lively", "Merry", "Polite", "Quick", "Royal", "Shiny",
		"Sunny", "Warm", "Wise", "Young", "Cosmic", "Golden", "Silent", "Velvet",
		"Amber", "Azure", "Crimson", "Daring", "Electric", "Frosty", "Hidden",
		"Ivory", "Jade", "Keen", "Lunar", "Mellow", "Nimble", "Olive", "Plucky",
		"Radiant", "Stellar", "Tranquil", "Urban", "Vivid", "Wandering", "Zesty",
	}
	olcrtcRoomNouns = []string{
		"Tigers", "Pandas", "Eagles", "Foxes", "Whales", "Otters", "Falcons",
		"Dragons", "Wolves", "Lions", "Bears", "Hawks", "Dolphins", "Rabbits",
		"Horses", "Comets", "Rivers", "Mountains", "Forests", "Gardens",
		"Beacons", "Lanterns", "Harbors", "Meadows", "Glaciers", "Canyons",
		"Willows", "Maples", "Cedars", "Orchids", "Sparrows", "Ravens",
		"Badgers", "Cranes", "Herons", "Jaguars", "Kestrels", "Lynxes",
		"Marmots", "Narwhals", "Ospreys", "Pumas", "Quails", "Stags",
		"Terns", "Vipers", "Walruses", "Yaks", "Zebras", "Bisons",
	}
	olcrtcRoomVerbs = []string{
		"Run", "Jump", "Dance", "Sing", "Dream", "Build", "Glide", "Wander",
		"Gather", "Explore", "Wonder", "Travel", "Sparkle", "Flourish",
		"Whisper", "Shine", "Drift", "Soar", "Bloom", "Gleam", "Roam", "Leap",
		"Race", "Climb", "Dive", "Float", "Gallop", "Hover", "Mingle", "Prowl",
		"Ramble", "Scatter", "Tumble", "Venture", "Wade", "Zoom",
	}
	olcrtcRoomAdverbs = []string{
		"Quietly", "Boldly", "Gently", "Swiftly", "Brightly", "Calmly",
		"Freely", "Happily", "Kindly", "Smoothly", "Warmly", "Wisely",
		"Eagerly", "Gracefully", "Proudly", "Quickly", "Softly", "Bravely",
		"Cheerfully", "Daily", "Eastward", "Gladly", "Lightly", "Merrily",
		"Nimbly", "Onward", "Plainly", "Readily", "Surely", "Truly",
	}
)

// randomRoomName returns a room name in one of several randomly chosen formats,
// so a panel's rooms do not all share one recognisable shape.
func randomRoomName() (string, error) {
	scheme, err := cryptoIntn(6)
	if err != nil {
		return "", err
	}
	switch scheme {
	case 0:
		// Four CamelCase words: "BraveLionsThinkLoudly".
		return camelWords(4)
	case 1:
		// Two or three CamelCase words: "BraveLions" / "BraveLionsRoam".
		n, err := cryptoIntn(2)
		if err != nil {
			return "", err
		}
		return camelWords(2 + n)
	case 2:
		// Words plus a short number: "BraveLions742".
		base, err := camelWords(2)
		if err != nil {
			return "", err
		}
		digits, err := randomDigits()
		if err != nil {
			return "", err
		}
		return base + digits, nil
	case 3:
		// Pure hex of varying length: "a3f9c2e1b4".
		return randomHexName()
	case 4:
		// Lowercase dash-separated words: "brave-lions-roam".
		return dashWords()
	default:
		// Mixed-case alphanumeric id: "x7Kp2mQ9rT".
		return randomAlphaNum()
	}
}

func camelWords(n int) (string, error) {
	lists := [][]string{olcrtcRoomAdjectives, olcrtcRoomNouns, olcrtcRoomVerbs, olcrtcRoomAdverbs}
	var b strings.Builder
	for i := 0; i < n && i < len(lists); i++ {
		w, err := pickWord(lists[i])
		if err != nil {
			return "", err
		}
		b.WriteString(w)
	}
	return b.String(), nil
}

func dashWords() (string, error) {
	lists := [][]string{olcrtcRoomAdjectives, olcrtcRoomNouns, olcrtcRoomVerbs}
	parts := make([]string, 0, len(lists))
	for _, list := range lists {
		w, err := pickWord(list)
		if err != nil {
			return "", err
		}
		parts = append(parts, strings.ToLower(w))
	}
	return strings.Join(parts, "-"), nil
}

func pickWord(list []string) (string, error) {
	idx, err := cryptoIntn(len(list))
	if err != nil {
		return "", err
	}
	return list[idx], nil
}

// randomDigits returns 2–4 decimal digits.
func randomDigits() (string, error) {
	extra, err := cryptoIntn(3) // 0..2 -> total 2..4 digits
	if err != nil {
		return "", err
	}
	count := 2 + extra
	var b strings.Builder
	for i := 0; i < count; i++ {
		d, err := cryptoIntn(10)
		if err != nil {
			return "", err
		}
		b.WriteByte(byte('0' + d))
	}
	return b.String(), nil
}

// randomHexName returns 8–16 hex characters.
func randomHexName() (string, error) {
	extra, err := cryptoIntn(5) // 0..4 -> 4..8 bytes -> 8..16 hex chars
	if err != nil {
		return "", err
	}
	b := make([]byte, 4+extra)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// randomAlphaNum returns an 8–12 char mixed-case alphanumeric id.
func randomAlphaNum() (string, error) {
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	extra, err := cryptoIntn(5) // 0..4 -> length 8..12
	if err != nil {
		return "", err
	}
	n := 8 + extra
	var b strings.Builder
	for i := 0; i < n; i++ {
		idx, err := cryptoIntn(len(alphabet))
		if err != nil {
			return "", err
		}
		b.WriteByte(alphabet[idx])
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
