package banner

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"
)

// passwordAlphabet excludes characters people commonly mistranscribe by
// hand or read wrong aloud: 0/o, 1/l/i. 31 symbols, not a round 32 - the
// alphabet is defined by what's unambiguous, not by a target size.
const passwordAlphabet = "23456789abcdefghjkmnpqrstuvwxyz"

// GeneratePassword returns a locally-generated password in three
// dash-separated groups of four characters (e.g. "k4pq-9wrt-2vnm"),
// drawn from passwordAlphabet via crypto/rand. 12 draws from a 31-symbol
// alphabet is about 59 bits of entropy - plenty for a page password, and
// short enough to read aloud or copy by hand without transcription
// errors.
func GeneratePassword() (string, error) {
	const groups = 3
	const groupLen = 4

	alphabetSize := big.NewInt(int64(len(passwordAlphabet)))
	var b strings.Builder
	for g := 0; g < groups; g++ {
		if g > 0 {
			b.WriteByte('-')
		}
		for i := 0; i < groupLen; i++ {
			idx, err := rand.Int(rand.Reader, alphabetSize)
			if err != nil {
				return "", fmt.Errorf("generating password: %w", err)
			}
			b.WriteByte(passwordAlphabet[idx.Int64()])
		}
	}
	return b.String(), nil
}
