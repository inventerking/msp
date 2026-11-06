package identity

import "strings"

// commonPasswords is a small bundled list checked offline, so a password is never sent outside
// the system. Only entries of 12 characters or more matter, because shorter ones already fail.
var commonPasswords = map[string]bool{}

func init() {
	for _, p := range strings.Fields(`
123456789012 1234567890123 12345678901234 123123123123 111111111111 000000000000
password1234 password12345 password123456 passwordpassword qwertyuiop12 qwertyuiop123
qwertyuiop1234 qwerty123456 qwerty1234567 1q2w3e4r5t6y 1qaz2wsx3edc 1qaz2wsx3edc4rfv
adminadmin12 administrator admin1234567 letmeinletmein welcome12345 welcome123456
iloveyou1234 changeme1234 changemenow1 trustno1trustno1 abcdefghijkl abcdefghijklm
abc123abc123 aaaaaaaaaaaa asdfghjkl123 asdfghjkl1234 zxcvbnm12345 zxcvbnm123456
monkeymonkey dragondragon football1234 baseball1234 superman1234 sunshine1234
masterkey123 passw0rd1234 p@ssw0rd1234 p@ssword1234 pa55word1234 mspgrowthengine
`) {
		commonPasswords[p] = true
	}
}

func isCommonPassword(p string) bool {
	l := strings.ToLower(p)
	if commonPasswords[l] {
		return true
	}
	// A single repeated character is never acceptable.
	return strings.Count(l, l[:1]) == len(l)
}
