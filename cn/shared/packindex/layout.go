package packindex

// The published layout, the same on the CDN, on ClaudinitePacks' vendored
// branch and on any repo mirroring it.
const (
	VendoredBranch = "vendored"
	CatalogFile    = "catalog.json"
	CatalogSigFile = "catalog.sig.json"
)

// IndexFile is pack id's index.json.
func IndexFile(id string) string { return id + "/index.json" }

// IndexSigFile is pack id's index.sig.json.
func IndexSigFile(id string) string { return id + "/index.sig.json" }

// ArchiveFile is one version's archive.
func ArchiveFile(id, version string) string { return id + "/" + version + ".tar.gz" }
