package license

// States is what a key says about engine releases and pack indexes, for
// the nightly update: held and revoked versions (each with its reason),
// the security fixes, the pack index serial floor and the accepted
// pack-index key ids.
type States struct {
	Held          map[string]string
	Revoked       map[string]string
	SecurityFixes []string
	SerialFloor   int64
	PackKeys      []string
}

// KeyReason is the reason a version held or revoked by a key carries.
const KeyReason = "license key"

// ReleaseStates reads a key's release block; a nil key holds nothing,
// floors nothing and accepts every pack key the roots certify.
func ReleaseStates(key *KeyPayload) States {
	s := States{Held: map[string]string{}, Revoked: map[string]string{}}
	if key == nil {
		return s
	}
	for _, v := range key.Release.Held {
		s.Held[v] = KeyReason
	}
	for _, v := range key.Release.Revoked {
		s.Revoked[v] = KeyReason
	}
	s.SecurityFixes = append(s.SecurityFixes, key.Release.SecurityFixes...)
	s.SerialFloor = key.Release.PackIndexSerial
	s.PackKeys = append(s.PackKeys, key.Release.PackKeys...)
	return s
}
