package p1

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
)

// Precomputed offline from the exact built-in synthetic fixtures.
// Each SHA-256 digest covers, in order: material ID, version, paragraph ID,
// date, speaker, and text. Each UTF-8 field is prefixed with its byte length
// as an unsigned 64-bit big-endian integer. No paragraph body is stored here.
// Fixture edits require explicit fingerprint regeneration, never runtime reload.
const (
	fixtureSAP01Fingerprint = "f991734a4c255a82418a21567c347f699b4fd5b6531afebb8ccd823e3c568984"
	fixtureSAP02Fingerprint = "2b6e965646860cd3aaf4082dfffb710e8f48c1f537f483bbf6f5f546c7dd9382"
	fixtureSBP01Fingerprint = "6bd506c61edf0362a76c2fbd6907e13f7098b2b384c51f855cd01ce1032c963f"
	fixtureSBP02Fingerprint = "d96bbcc6afe75cdbc39e24c3dd227d759b3ba2ee2b8c8a0bf44ab513169e3c86"
	fixtureSBP03Fingerprint = "98a9b6ee38dc77d6ee111fdc8064a4bc89eda1a042ff7a83871f77120c03b284"
	fixtureSCP01Fingerprint = "b3d77eef608e4287c1399548db0e36c005ab3fb6c3eabf610464c4f75b379cbf"
)

// expectedFixtureParagraphFingerprint recognizes only exact fixture identities.
// Returning an empty string means this version/paragraph supplies no evidence.
func expectedFixtureParagraphFingerprint(ref Ref, paragraphID string) string {
	if ref.Version != "v1" {
		return ""
	}
	switch ref.ID {
	case "S-A":
		switch paragraphID {
		case "P01":
			return fixtureSAP01Fingerprint
		case "P02":
			return fixtureSAP02Fingerprint
		}
	case "S-B":
		switch paragraphID {
		case "P01":
			return fixtureSBP01Fingerprint
		case "P02":
			return fixtureSBP02Fingerprint
		case "P03":
			return fixtureSBP03Fingerprint
		}
	case "S-C":
		switch paragraphID {
		case "P01":
			return fixtureSCP01Fingerprint
		}
	}
	return ""
}

// suppliedParagraphFingerprint reads only the paragraph supplied by the caller.
// Length prefixes prevent differently partitioned fields sharing an encoding.
func suppliedParagraphFingerprint(ref Ref, paragraph Paragraph) string {
	digest := sha256.New()
	for _, value := range []string{ref.ID, ref.Version, paragraph.ID, paragraph.Date, paragraph.Speaker, paragraph.Text} {
		var length [8]byte
		binary.BigEndian.PutUint64(length[:], uint64(len(value)))
		digest.Write(length[:])
		digest.Write([]byte(value))
	}
	return hex.EncodeToString(digest.Sum(nil))
}
