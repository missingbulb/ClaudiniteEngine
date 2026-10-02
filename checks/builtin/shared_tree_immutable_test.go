package builtin

import "testing"

func TestSharedTreeImmutable(t *testing.T) {
	edit := map[string]string{".claudinite/shared/packs/hello/RULES.md": "# hello\n", "a.txt": "b\n"}
	base := map[string]string{"a.txt": "a\n"}
	expect(t, repo{base: base, change: edit, message: "Edit the vendored hello pack"}.run(t, "shared-tree-immutable"),
		want{path: ".claudinite/shared/packs/hello/RULES.md", what: `^this branch commits \.claudinite/shared/packs/hello/RULES\.md, inside the vendored \.claudinite/shared/ tree, under a title the update task does not compose$`, fix: `\.claudinite/local/packs/`, advise: true})
	expect(t, repo{base: base, change: edit, message: "Claudinite update: refresh the mount"}.run(t, "shared-tree-immutable"))
	expect(t, repo{base: base, change: map[string]string{"a.txt": "b\n"}}.run(t, "shared-tree-immutable"))
}

func TestSharedTreeImmutableIsSilentOnTheDefaultBranch(t *testing.T) {
	r := repo{base: map[string]string{".claudinite/shared/packs/hello/RULES.md": "# hello\n"}}
	expect(t, r.run(t, "shared-tree-immutable"))
}
