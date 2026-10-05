package update

import (
	"github.com/missingbulb/ClaudiniteEngine/shared/flatdecl"
	"github.com/missingbulb/ClaudiniteEngine/shared/packset"
)

// writeFlat writes the flat files the repo's declared packs produce under
// its current pin, returning the paths that changed: an update PR carries
// them, since the member file states the pin and the held versions an
// update moves, and a moved pack's tasks are what the task file copies.
func writeFlat(repo string) ([]string, error) {
	set, err := packset.Load(repo, pinVersion(repo), false)
	if err != nil {
		return nil, err
	}
	return flatdecl.Write(repo, set.Packs)
}

// writeMemberFile writes the member file alone, for the engine update PR,
// which moves the pin and nothing a task or descriptor copy reads,
// returning the path written or "". A declaration the parser refuses
// leaves the file alone: the pin still moves, and verify names the
// declaration's fault.
func writeMemberFile(repo string) (string, error) {
	set, err := packset.Load(repo, pinVersion(repo), false)
	if err != nil || len(set.Packs) == 0 {
		return "", nil
	}
	return flatdecl.WriteMember(repo, set.Packs)
}

// isFlatFile reports whether file is one of the flat files writeFlat
// writes.
func isFlatFile(file string) bool {
	for _, f := range flatdecl.Files {
		if f == file {
			return true
		}
	}
	return false
}

// isMemberFile reports whether file is the member file where a member
// may hold it.
func isMemberFile(file string) bool {
	return file == flatdecl.MemberFile || file == flatdecl.LegacyPath(flatdecl.MemberFile)
}
